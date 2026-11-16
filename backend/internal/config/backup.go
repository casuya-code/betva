package config

import (
	"os"
	"strings"
)

// BackupConfig is the environment for cmd/backup, the scheduled off-site
// Postgres dump job. It is separate from the API config so the API image never
// needs object-storage credentials.
type BackupConfig struct {
	DatabaseURL string

	// S3-compatible destination (Railway bucket, Cloudflare R2, AWS S3).
	Bucket   string
	Endpoint string
	Region   string
	Prefix   string

	// Optional second provider (Cloudflare R2) with separate credentials.
	// When configured, every dump and manifest is mirrored there and both
	// buckets are pruned. This survives a Railway account-level outage.
	SecondaryBucket   string
	SecondaryEndpoint string
	SecondaryRegion   string
	SecondaryKeyID    string
	SecondarySecret   string

	// Grandfather-father-son retention, in days.
	DailyKeepDays   int
	WeeklyKeepDays  int
	MonthlyKeepDays int

	// Binaries. Defaults assume the postgres image, which ships pg_dump 18.
	PgDump    string
	PgRestore string
}

// LoadBackup reads the backup job environment. Missing required values are
// reported by the caller, not silently defaulted.
func LoadBackup() *BackupConfig {
	return &BackupConfig{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		Bucket:            os.Getenv("BACKUP_S3_BUCKET"),
		Endpoint:          os.Getenv("BACKUP_S3_ENDPOINT"),
		Region:            getEnv("BACKUP_S3_REGION", "auto"),
		Prefix:            strings.Trim(getEnv("BACKUP_PREFIX", "db"), "/"),
		DailyKeepDays:     Atoi(getEnv("BACKUP_DAILY_KEEP_DAYS", "14"), 14),
		WeeklyKeepDays:    Atoi(getEnv("BACKUP_WEEKLY_KEEP_DAYS", "56"), 56),
		MonthlyKeepDays:   Atoi(getEnv("BACKUP_MONTHLY_KEEP_DAYS", "183"), 183),
		PgDump:            getEnv("PG_DUMP", "pg_dump"),
		PgRestore:         getEnv("PG_RESTORE", "pg_restore"),
		SecondaryBucket:   os.Getenv("BACKUP_R2_BUCKET"),
		SecondaryEndpoint: os.Getenv("BACKUP_R2_ENDPOINT"),
		SecondaryRegion:   getEnv("BACKUP_R2_REGION", "auto"),
		SecondaryKeyID:    os.Getenv("R2_ACCESS_KEY_ID"),
		SecondarySecret:   os.Getenv("R2_SECRET_ACCESS_KEY"),
	}
}

// Missing returns the names of required settings that are empty.
func (c *BackupConfig) Missing() []string {
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"DATABASE_URL", c.DatabaseURL},
		{"BACKUP_S3_BUCKET", c.Bucket},
		{"AWS_ACCESS_KEY_ID", os.Getenv("AWS_ACCESS_KEY_ID")},
		{"AWS_SECRET_ACCESS_KEY", os.Getenv("AWS_SECRET_ACCESS_KEY")},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	return missing
}

// HasSecondary reports whether a second-provider mirror is configured. All
// five settings are required; a partial configuration is an error at startup.
func (c *BackupConfig) HasSecondary() bool {
	return c.SecondaryBucket != "" || c.SecondaryEndpoint != "" ||
		c.SecondaryKeyID != "" || c.SecondarySecret != ""
}

// SecondaryMissing returns settings needed to complete the mirror.
func (c *BackupConfig) SecondaryMissing() []string {
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"BACKUP_R2_BUCKET", c.SecondaryBucket},
		{"BACKUP_R2_ENDPOINT", c.SecondaryEndpoint},
		{"R2_ACCESS_KEY_ID", c.SecondaryKeyID},
		{"R2_SECRET_ACCESS_KEY", c.SecondarySecret},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	return missing
}
