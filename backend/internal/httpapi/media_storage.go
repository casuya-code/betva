package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/betva/backend/internal/config"
)

// mediaStore abstracts where uploaded community media lives: local disk by
// default, Cloudflare R2 when configured. The returned/stored URL is what the
// client saves in a post, so switching storage backends needs no frontend change.
//
// Delivery modes when R2 is enabled:
//   - Public: R2MediaPublicBase set and presigning off → store/serve the public
//     base URL directly (best for public content, zero backend cost on playback;
//     works with r2.dev or a custom domain).
//   - Presigned: R2MediaPresignTTL > 0 → store an r2://<bucket>/<key> reference
//     and generate short-lived GET URLs on read (minimal backend cost, suitable
//     for private buckets/content without a custom domain).
type mediaStore struct {
	localDir string

	r2Enabled    bool
	r2Client     *s3.Client
	r2Presigner  *s3.PresignClient
	r2Bucket     string
	r2PublicBase string
	r2PresignTTL time.Duration
}

var localMediaRe = regexp.MustCompile(`^/media/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp|avif|gif|mp4|webm)$`)
var localProfileMediaRe = regexp.MustCompile(`^/media/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp|avif|gif)$`)

func newMediaStore(cfg *config.Config) *mediaStore {
	ms := &mediaStore{localDir: cfg.MediaDir}
	if cfg.R2AccountID == "" || cfg.R2AccessKeyID == "" ||
		cfg.R2SecretAccessKey == "" || cfg.R2MediaBucket == "" {
		return ms
	}
	presignTTL := time.Duration(cfg.R2MediaPresignTTL) * time.Second
	if cfg.R2MediaPublicBase == "" && presignTTL <= 0 {
		return ms
	}
	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.R2AccessKeyID, cfg.R2SecretAccessKey, "",
		)),
	)
	if err != nil {
		return ms
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID))
		o.UsePathStyle = false
	})
	ms.r2Enabled = true
	ms.r2Client = client
	ms.r2Bucket = cfg.R2MediaBucket
	ms.r2PublicBase = strings.TrimRight(cfg.R2MediaPublicBase, "/")
	if presignTTL > 0 {
		ms.r2PresignTTL = presignTTL
		ms.r2Presigner = s3.NewPresignClient(client)
	}
	return ms
}

func (m *mediaStore) r2KeyPrefix() string {
	if m == nil || m.r2Bucket == "" {
		return ""
	}
	return "r2://" + m.r2Bucket + "/"
}

// allowsPostURL reports whether a media reference was issued by this store.
// A post may only point at a local /media/ file, the configured R2 public
// bucket base, or an internal r2:// object reference used with presigned
// delivery — never at an arbitrary remote URL, which would turn the feed into
// a tracking surface.
func (m *mediaStore) allowsPostURL(raw string) bool {
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "?#") {
		return false
	}
	if localMediaRe.MatchString(raw) {
		return true
	}
	if m != nil && m.r2Enabled && m.r2PublicBase != "" {
		prefix := m.r2PublicBase + "/media/"
		return strings.HasPrefix(raw, prefix) && len(raw) > len(prefix)
	}
	if m != nil && m.r2Enabled && m.r2Presigner != nil {
		prefix := m.r2KeyPrefix() + "media/"
		return strings.HasPrefix(raw, prefix) && len(raw) > len(prefix)
	}
	return false
}

// allowsProfilePic reports whether a profile picture reference was issued by
// this store. Only image types are allowed, and only from our own storage.
func (m *mediaStore) allowsProfilePic(raw string) bool {
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "?#") {
		return false
	}
	if localProfileMediaRe.MatchString(raw) {
		return true
	}
	if m != nil && m.r2Enabled && m.r2PublicBase != "" {
		prefix := m.r2PublicBase + "/media/"
		if !strings.HasPrefix(raw, prefix) || len(raw) <= len(prefix) {
			return false
		}
		name := raw[len(prefix):]
		return localProfileMediaRe.MatchString("/media/" + name)
	}
	if m != nil && m.r2Enabled && m.r2Presigner != nil {
		prefix := m.r2KeyPrefix() + "media/"
		if !strings.HasPrefix(raw, prefix) || len(raw) <= len(prefix) {
			return false
		}
		name := raw[len(prefix):]
		return localProfileMediaRe.MatchString("/media/" + name)
	}
	return false
}

// displayURL returns a client-usable URL for a stored media reference. For R2
// presigned mode it generates a short-lived GET URL; otherwise it returns the
// stored URL unchanged (local /media path or public R2 URL).
func (m *mediaStore) displayURL(ctx context.Context, raw string) string {
	if raw == "" || m == nil || m.r2Presigner == nil || m.r2PresignTTL <= 0 {
		return raw
	}
	prefix := m.r2KeyPrefix()
	if !strings.HasPrefix(raw, prefix) {
		return raw
	}
	key := strings.TrimPrefix(raw, prefix)
	if key == "" {
		return raw
	}
	out, err := m.r2Presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(m.r2Bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = m.r2PresignTTL
	})
	if err != nil || out == nil || out.URL == "" {
		return raw
	}
	return out.URL
}

func (m *mediaStore) store(ctx context.Context, name string, data []byte, contentType string) (string, error) {
	if m.r2Enabled {
		key := "media/" + name
		_, err := m.r2Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:       aws.String(m.r2Bucket),
			Key:          aws.String(key),
			Body:         bytes.NewReader(data),
			ContentType:  aws.String(contentType),
			CacheControl: aws.String("public, max-age=31536000, immutable"),
		})
		if err != nil {
			return "", err
		}
		if m.r2Presigner != nil && m.r2PresignTTL > 0 {
			return m.r2KeyPrefix() + key, nil
		}
		return m.r2PublicBase + "/" + key, nil
	}

	if err := os.MkdirAll(m.localDir, 0o755); err != nil {
		return "", err
	}
	dst, err := os.Create(filepath.Join(m.localDir, name))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, bytes.NewReader(data)); err != nil {
		dst.Close()
		_ = os.Remove(filepath.Join(m.localDir, name))
		return "", err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(filepath.Join(m.localDir, name))
		return "", err
	}
	return "/media/" + name, nil
}