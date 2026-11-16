// Command backup creates a logical Postgres dump, verifies it, uploads it to
// S3-compatible object storage, and prunes expired copies.
//
// It runs as a Railway cron service (see backend/Dockerfile.backup), once a day.
// Every run is independent: no state is kept between runs, and a failed run
// exits non-zero so the platform marks it failed.
//
// Object layout:
//
//	<prefix>/YYYY/MM/DD/betva-YYYYMMDDTHHMMSSZ.dump       pg_dump custom format
//	<prefix>/YYYY/MM/DD/betva-YYYYMMDDTHHMMSSZ.dump.json  manifest (size, sha256)
//
// Retention is grandfather-father-son: daily dumps for BACKUP_DAILY_KEEP_DAYS,
// Sunday dumps for BACKUP_WEEKLY_KEEP_DAYS, and first-of-month dumps for
// BACKUP_MONTHLY_KEEP_DAYS. Nothing newer than 24 hours is ever deleted.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/betva/backend/internal/config"
	"github.com/joho/godotenv"
)

type manifest struct {
	CreatedAt    string `json:"created_at"`
	Key          string `json:"key"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	PgDump       string `json:"pg_dump_version"`
	DatabaseHost string `json:"database_host"`
	Tables       int    `json:"tables"`
	Retention    struct {
		DailyDays   int `json:"daily_days"`
		WeeklyDays  int `json:"weekly_days"`
		MonthlyDays int `json:"monthly_days"`
	} `json:"retention"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	_ = godotenv.Load()

	list := flag.Bool("list", false, "list stored dumps and exit")
	secondary := flag.Bool("secondary", false, "use the R2 mirror for -list/-download/-delete")
	deleteKey := flag.String("delete", "", "delete one stored dump by exact key and exit")
	downloadKey := flag.String("download", "", "download one stored dump by exact key and exit")
	outPath := flag.String("out", "", "destination file for -download")
	flag.Parse()

	cfg := config.LoadBackup()
	if missing := cfg.Missing(); len(missing) > 0 {
		log.Fatalf("backup: missing required env: %s", strings.Join(missing, ", "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client, err := newS3(ctx, cfg)
	if err != nil {
		log.Fatalf("backup: s3 client: %v", err)
	}

	// Optional mirror on a second provider. A partial configuration is fatal
	// so a typo cannot silently disable the off-site copy.
	var mirror *s3.Client
	if cfg.HasSecondary() {
		if missing := cfg.SecondaryMissing(); len(missing) > 0 {
			log.Fatalf("backup: incomplete R2 mirror config: %s", strings.Join(missing, ", "))
		}
		mirror, err = newS3With(ctx, cfg.SecondaryEndpoint, cfg.SecondaryRegion, cfg.SecondaryKeyID, cfg.SecondarySecret)
		if err != nil {
			log.Fatalf("backup: r2 client: %v", err)
		}
	}

	bucket := cfg.Bucket
	if *secondary {
		if mirror == nil {
			log.Fatal("backup: no R2 mirror is configured")
		}
		client, bucket = mirror, cfg.SecondaryBucket
	}

	if *list {
		if err := listDumps(ctx, client, bucket, cfg); err != nil {
			log.Fatalf("backup: list: %v", err)
		}
		return
	}

	if *downloadKey != "" {
		if *outPath == "" {
			log.Fatal("backup: -download requires -out <path>")
		}
		if err := downloadDump(ctx, client, bucket, cfg, *downloadKey, *outPath); err != nil {
			log.Fatalf("backup: download: %v", err)
		}
		return
	}

	if *deleteKey != "" {
		if !strings.HasPrefix(*deleteKey, cfg.Prefix+"/") || !strings.HasSuffix(*deleteKey, ".dump") {
			log.Fatalf("backup: -delete must be a .dump key under %s/", cfg.Prefix)
		}
		for _, k := range []string{*deleteKey, *deleteKey + ".json"} {
			if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(k),
			}); err != nil {
				log.Fatalf("backup: delete %s: %v", k, err)
			}
		}
		log.Printf("backup: deleted %s", *deleteKey)
		return
	}

	started := time.Now().UTC()
	dumpPath, err := createDump(cfg, started)
	if err != nil {
		log.Fatalf("backup: %v", err)
	}
	defer os.Remove(dumpPath)

	tables, err := verifyDump(cfg, dumpPath)
	if err != nil {
		log.Fatalf("backup: %v", err)
	}
	if tables == 0 {
		log.Fatalf("backup: dump contains no table data; refusing to upload")
	}

	info, err := os.Stat(dumpPath)
	if err != nil {
		log.Fatalf("backup: stat dump: %v", err)
	}
	sum, err := sha256File(dumpPath)
	if err != nil {
		log.Fatalf("backup: checksum: %v", err)
	}

	key := fmt.Sprintf("%s/%s/betva-%s.dump",
		cfg.Prefix,
		started.Format("2006/01/02"),
		started.Format("20060102T150405Z"),
	)

	if err := upload(ctx, client, bucket, key, "application/octet-stream", dumpPath, info.Size()); err != nil {
		log.Fatalf("backup: upload dump: %v", err)
	}

	m := manifest{
		CreatedAt:    started.Format(time.RFC3339),
		Key:          key,
		SizeBytes:    info.Size(),
		SHA256:       sum,
		PgDump:       pgVersion(cfg),
		DatabaseHost: databaseHost(cfg.DatabaseURL),
		Tables:       tables,
	}
	m.Retention.DailyDays = cfg.DailyKeepDays
	m.Retention.WeeklyDays = cfg.WeeklyKeepDays
	m.Retention.MonthlyDays = cfg.MonthlyKeepDays

	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Fatalf("backup: marshal manifest: %v", err)
	}
	if err := uploadBytes(ctx, client, bucket, key+".json", "application/json", body); err != nil {
		log.Fatalf("backup: upload manifest: %v", err)
	}

	log.Printf("backup: uploaded %s to %s (%d bytes, %d tables, sha256 %s)", key, bucket, info.Size(), tables, sum[:12])

	if mirror != nil {
		if err := upload(ctx, mirror, cfg.SecondaryBucket, key, "application/octet-stream", dumpPath, info.Size()); err != nil {
			log.Fatalf("backup: R2 upload dump: %v", err)
		}
		if err := uploadBytes(ctx, mirror, cfg.SecondaryBucket, key+".json", "application/json", body); err != nil {
			log.Fatalf("backup: R2 upload manifest: %v", err)
		}
		log.Printf("backup: mirrored %s to %s", key, cfg.SecondaryBucket)
	}

	deleted, err := prune(ctx, client, bucket, cfg, started)
	if err != nil {
		log.Fatalf("backup: prune: %v", err)
	}
	if mirror != nil {
		mirrorDeleted, err := prune(ctx, mirror, cfg.SecondaryBucket, cfg, started)
		if err != nil {
			log.Fatalf("backup: R2 prune: %v", err)
		}
		deleted += mirrorDeleted
	}
	log.Printf("backup: done in %s; pruned %d expired dump(s)", time.Since(started).Round(time.Millisecond), deleted)
}

func newS3(ctx context.Context, cfg *config.BackupConfig) (*s3.Client, error) {
	return newS3With(ctx, cfg.Endpoint, cfg.Region,
		os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"))
}

// newS3With creates a virtual-host-style S3 client for a given endpoint and
// credential pair. Railway buckets and R2 both speak the same S3 API.
func newS3With(ctx context.Context, endpoint, region, keyID, secret string) (*s3.Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(keyID, secret, "")),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = false
	}), nil
}

// createDump writes a custom-format dump and returns its path.
func createDump(cfg *config.BackupConfig, at time.Time) (string, error) {
	dir, err := os.MkdirTemp("", "betva-backup-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	path := filepath.Join(dir, "betva.dump")

	cmd := exec.Command(cfg.PgDump,
		"--format=custom",
		"--compress=9",
		"--no-owner",
		"--no-privileges",
		"--file="+path,
		cfg.DatabaseURL,
	)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=15")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return path, nil
}

// verifyDump lists the archive and counts table-data entries. A truncated or
// unreadable dump fails here instead of being uploaded as a false backup.
func verifyDump(cfg *config.BackupConfig, path string) (int, error) {
	cmd := exec.Command(cfg.PgRestore, "--list", path)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("pg_restore --list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	tables := 0
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "TABLE DATA") {
			tables++
		}
	}
	return tables, nil
}

func pgVersion(cfg *config.BackupConfig) string {
	out, err := exec.Command(cfg.PgDump, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// databaseHost returns host:port without credentials, safe for a manifest.
func databaseHost(raw string) string {
	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return "unknown"
	}
	rest := raw[at+1:]
	if i := strings.IndexAny(rest, "/?"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func upload(ctx context.Context, client *s3.Client, bucket, key, contentType, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	return err
}

func uploadBytes(ctx context.Context, client *s3.Client, bucket, key, contentType string, body []byte) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
	})
	return err
}

// listDumps prints every stored dump with its size and age, newest first.
func listDumps(ctx context.Context, client *s3.Client, bucket string, cfg *config.BackupConfig) error {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(cfg.Prefix + "/"),
	})
	type row struct {
		key  string
		size int64
		when time.Time
	}
	var rows []row
	var total int64
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, ".dump") {
				continue
			}
			when, ok := keyTime(key)
			if !ok {
				continue
			}
			rows = append(rows, row{key: key, size: aws.ToInt64(obj.Size), when: when})
			total += aws.ToInt64(obj.Size)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].when.After(rows[j].when) })
	for _, r := range rows {
		fmt.Printf("%s  %10d B  %s\n", r.when.Format(time.RFC3339), r.size, r.key)
	}
	fmt.Printf("%d dump(s), %d bytes total\n", len(rows), total)
	return nil
}

// downloadDump fetches a dump and verifies its size and SHA-256 against the
// manifest written at upload time. A corrupt download never lands silently.
func downloadDump(ctx context.Context, client *s3.Client, bucket string, cfg *config.BackupConfig, key, out string) error {
	if !strings.HasPrefix(key, cfg.Prefix+"/") || !strings.HasSuffix(key, ".dump") {
		return fmt.Errorf("key must be a .dump under %s/", cfg.Prefix)
	}

	manifestObj, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key + ".json"),
	})
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	defer manifestObj.Body.Close()
	var m manifest
	if err := json.NewDecoder(manifestObj.Body).Decode(&m); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	obj, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("dump: %w", err)
	}
	defer obj.Body.Close()

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), obj.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	sum := hex.EncodeToString(h.Sum(nil))
	if m.SizeBytes != 0 && n != m.SizeBytes {
		return fmt.Errorf("size mismatch: got %d bytes, manifest says %d", n, m.SizeBytes)
	}
	if m.SHA256 != "" && sum != m.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s, manifest says %s", sum, m.SHA256)
	}
	log.Printf("backup: downloaded %s to %s (%d bytes, sha256 verified)", key, out, n)
	return nil
}

// prune applies GFS retention. The dump timestamp comes from the key path, so
// pruning needs no metadata calls.
func prune(ctx context.Context, client *s3.Client, bucket string, cfg *config.BackupConfig, now time.Time) (int, error) {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(cfg.Prefix + "/"),
	})

	deleted := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return deleted, err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, ".dump") {
				continue
			}
			when, ok := keyTime(key)
			if !ok {
				continue
			}
			if keepDump(when, now, cfg) {
				continue
			}
			for _, k := range []string{key, key + ".json"} {
				_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(bucket),
					Key:    aws.String(k),
				})
				if err != nil {
					return deleted, fmt.Errorf("delete %s: %w", k, err)
				}
			}
			deleted++
			log.Printf("backup: pruned %s", key)
		}
	}
	return deleted, nil
}

// keyTime reads the timestamp from <prefix>/YYYY/MM/DD/betva-YYYYMMDDTHHMMSSZ.dump.
func keyTime(key string) (time.Time, bool) {
	base := strings.TrimSuffix(filepath.Base(key), ".dump")
	base = strings.TrimPrefix(base, "betva-")
	t, err := time.Parse("20060102T150405Z", base)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func keepDump(when, now time.Time, cfg *config.BackupConfig) bool {
	age := now.Sub(when)
	if age < 24*time.Hour {
		return true // never delete anything from the last day
	}
	day := 24 * time.Hour
	if age <= time.Duration(cfg.DailyKeepDays)*day {
		return true
	}
	if when.Weekday() == time.Sunday && age <= time.Duration(cfg.WeeklyKeepDays)*day {
		return true
	}
	if when.Day() == 1 && age <= time.Duration(cfg.MonthlyKeepDays)*day {
		return true
	}
	return false
}
