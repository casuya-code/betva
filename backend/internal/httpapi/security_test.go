package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToMax(t *testing.T) {
	l := NewRateLimiter(2, time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first request should be allowed")
	}
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("second request should be allowed")
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("third request should be limited")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("unexpected retry duration: %s", retry)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("a different key should have its own window")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	var l *RateLimiter
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("nil limiter should allow")
	}
	if ok, _ := NewRateLimiter(0, time.Minute).Allow("a"); !ok {
		t.Fatal("max=0 should disable the limiter")
	}
}

func TestRateLimitWrapReturns429(t *testing.T) {
	l := NewRateLimiter(1, time.Minute)
	h := l.Wrap(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	first := httptest.NewRecorder()
	h(first, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first request: got %d", first.Code)
	}

	second := httptest.NewRecorder()
	h(second, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("429 should carry Retry-After")
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders("production")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/config", nil))

	for _, name := range []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Content-Security-Policy",
		"Strict-Transport-Security",
	} {
		if rec.Header().Get(name) == "" {
			t.Errorf("missing header %s", name)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("api cache-control = %q, want no-store", got)
	}

	media := httptest.NewRecorder()
	h.ServeHTTP(media, httptest.NewRequest("GET", "/media/abc.jpg", nil))
	if got := media.Header().Get("Cache-Control"); got == "no-store" {
		t.Error("media responses must keep their immutable cache header")
	}
}
