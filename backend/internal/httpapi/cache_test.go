package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestJSONCacheStoresAndExpires(t *testing.T) {
	c := newJSONCache("test:stats", time.Second, nil)
	ctx := context.Background()

	if _, ok := c.Get(ctx); ok {
		t.Fatal("empty cache should miss")
	}

	c.Set(ctx, []byte(`{"total":1}`))
	if got, ok := c.Get(ctx); !ok || string(got) != `{"total":1}` {
		t.Fatalf("cache hit mismatch: ok=%v got=%s", ok, got)
	}

	// A zero/negative TTL makes the value immediately stale.
	c.at = time.Now().Add(-2 * time.Second)
	if _, ok := c.Get(ctx); ok {
		t.Fatal("expired value should miss")
	}
}

func TestJSONCacheNilIsNoop(t *testing.T) {
	var c *jsonCache
	ctx := context.Background()
	if _, ok := c.Get(ctx); ok {
		t.Fatal("nil cache should miss")
	}
	c.Set(ctx, []byte("x")) // must not panic
}

func TestSharedRateLimiterFallsBackWithoutRedis(t *testing.T) {
	// With no Redis client the shared limiter must behave exactly like the
	// local one, so Redis being unavailable never removes the cap.
	l := NewSharedRateLimiter(1, time.Minute, nil, "test")
	ctx := context.Background()
	if ok, _ := l.AllowCtx(ctx, "k"); !ok {
		t.Fatal("first request should be allowed")
	}
	if ok, _ := l.AllowCtx(ctx, "k"); ok {
		t.Fatal("second request should be limited")
	}
	if ok, _ := l.AllowCtx(ctx, "other"); !ok {
		t.Fatal("different key should have its own window")
	}
}
