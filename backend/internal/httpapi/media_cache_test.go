package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaCacheImmutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.jpg"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := mediaCache(http.FileServer(http.Dir(dir)))

	for _, tc := range []struct {
		path      string
		want      int
		wantCache bool
	}{{"/abc.jpg", http.StatusOK, true}, {"/missing.jpg", http.StatusNotFound, false}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.path, rec.Code, tc.want)
		}
		got := rec.Header().Get("Cache-Control")
		if tc.wantCache && got != "public, max-age=31536000, immutable" {
			t.Errorf("%s: cache-control = %q", tc.path, got)
		}
		if !tc.wantCache && got != "" {
			t.Errorf("%s: 404 should not claim immutable caching, got %q", tc.path, got)
		}
	}
}
