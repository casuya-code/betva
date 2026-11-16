package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/betva/backend/internal/config"
)

// mediaStore abstracts where uploaded community media lives: local disk by
// default, Cloudflare R2 when configured. The returned URL is what the client
// stores in a post, so switching storage backends needs no frontend change.
type mediaStore struct {
	localDir string

	r2Enabled     bool
	r2Client      *s3.Client
	r2Bucket      string
	r2PublicBase  string
}

func newMediaStore(cfg *config.Config) *mediaStore {
	ms := &mediaStore{localDir: cfg.MediaDir}
	if cfg.R2AccountID == "" || cfg.R2AccessKeyID == "" ||
		cfg.R2SecretAccessKey == "" || cfg.R2MediaBucket == "" ||
		cfg.R2MediaPublicBase == "" {
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
	return ms
}

func (m *mediaStore) store(ctx context.Context, name string, data []byte, contentType string) (string, error) {
	if m.r2Enabled {
		_, err := m.r2Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:       aws.String(m.r2Bucket),
			Key:          aws.String("media/" + name),
			Body:         bytes.NewReader(data),
			ContentType:  aws.String(contentType),
			CacheControl: aws.String("public, max-age=31536000, immutable"),
		})
		if err != nil {
			return "", err
		}
		return m.r2PublicBase + "/media/" + name, nil
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