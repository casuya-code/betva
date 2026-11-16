package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/betva/backend/internal/config"
)

func TestMediaStoreLocal(t *testing.T) {
	dir := t.TempDir()
	ms := &mediaStore{localDir: dir}

	data := []byte("hello image")
	url, err := ms.store(context.Background(), "abc.jpg", data, "image/jpeg")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if url != "/media/abc.jpg" {
		t.Fatalf("url = %q", url)
	}
	got, err := os.ReadFile(filepath.Join(dir, "abc.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("file content mismatch")
	}
}

func TestNewMediaStoreDisabledWithoutR2(t *testing.T) {
	cfg := &config.Config{MediaDir: t.TempDir()}
	ms := newMediaStore(cfg)
	if ms.r2Enabled {
		t.Fatal("expected R2 disabled when credentials missing")
	}
	if ms.localDir != cfg.MediaDir {
		t.Fatalf("localDir = %q", ms.localDir)
	}
}

func TestNewMediaStoreEnabledWithR2(t *testing.T) {
	cfg := &config.Config{
		MediaDir:          t.TempDir(),
		R2AccountID:       "acct",
		R2AccessKeyID:     "key",
		R2SecretAccessKey: "secret",
		R2MediaBucket:     "bucket",
		R2MediaPublicBase: "https://pub.example.r2.dev",
	}
	ms := newMediaStore(cfg)
	if !ms.r2Enabled {
		t.Fatal("expected R2 enabled when all credentials present")
	}
	if ms.r2Bucket != "bucket" {
		t.Fatalf("bucket = %q", ms.r2Bucket)
	}
	if ms.r2PublicBase != "https://pub.example.r2.dev" {
		t.Fatalf("base = %q", ms.r2PublicBase)
	}
}