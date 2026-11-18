package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteCachedJSONETagAnd304(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Origin")
		writeCachedJSON(w, r, http.StatusOK, "private, max-age=60", true, map[string]string{"a": "b"})
	})

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/matches", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if got := first.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Fatalf("Cache-Control = %q", got)
	}
	vary := first.Header().Values("Vary")
	if len(vary) != 2 || vary[0] != "Origin" || vary[1] != "Authorization" {
		t.Fatalf("Vary = %v, want [Origin Authorization]", vary)
	}
	if first.Body.Len() == 0 {
		t.Fatal("200 response has no body")
	}

	second := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/matches", nil)
	req.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 must have no body, got %q", second.Body.String())
	}
	if second.Header().Get("ETag") != etag {
		t.Fatal("304 lost the ETag")
	}
}

func TestWriteCachedJSONETagTracksBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preview := r.URL.Query().Get("preview") == "1"
		writeCachedJSON(w, r, http.StatusOK, "private, max-age=60", true, map[string]interface{}{
			"preview": preview,
		})
	})

	full := httptest.NewRecorder()
	handler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/api/v1/matches", nil))
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "/api/v1/matches?preview=1", nil))

	if full.Header().Get("ETag") == preview.Header().Get("ETag") {
		t.Fatal("different bodies must have different ETags")
	}
}

func TestWriteCachedJSONIgnoresNonMatchingETag(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeCachedJSON(w, r, http.StatusOK, "public, max-age=60", false, map[string]string{"ok": "yes"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("If-None-Match", `"stale"`)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.Len() == 0 {
		t.Fatalf("stale ETag should return 200 with a body, got %d / %q", res.Code, res.Body.String())
	}
}
