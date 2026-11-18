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

func TestAllowsPostURL(t *testing.T) {
	local := &mediaStore{}
	r2 := &mediaStore{r2Enabled: true, r2PublicBase: "https://pub.example.r2.dev"}

	cases := []struct {
		name string
		ms   *mediaStore
		url  string
		want bool
	}{
		{"local", local, "/media/abc.jpg", true},
		{"local missing name", local, "/media/", false},
		{"local traversal", local, "/media/../secret.jpg", false},
		{"r2", r2, "https://pub.example.r2.dev/media/abc.jpg", true},
		{"r2 query", r2, "https://pub.example.r2.dev/media/abc.jpg?x=1", false},
		{"r2 lookalike host", r2, "https://pub.example.r2.dev.evil.com/media/abc.jpg", false},
		{"r2 disabled", local, "https://pub.example.r2.dev/media/abc.jpg", false},
		{"arbitrary remote", r2, "https://example.com/tracker.gif", false},
		{"javascript", r2, "javascript:alert(1)", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ms.allowsPostURL(tc.url); got != tc.want {
				t.Fatalf("allowsPostURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}