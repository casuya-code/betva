package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// jsonCache stores a small, public JSON blob for a short time. It has two
// layers:
//
//   - an in-memory value, which keeps reads fast (no network hop) and works
//     when Redis is unavailable;
//   - an optional Redis key, which makes the cache shared across API replicas.
//
// The counter behind /stats only changes at settlement, so a 60-second cache
// saves a full-table aggregation on every cache miss without showing stale
// results for meaningfully long. Values are public and contain no user data.
type jsonCache struct {
	key string
	ttl time.Duration
	rdb *redis.Client

	mu    sync.RWMutex
	value []byte
	at    time.Time
}

func newJSONCache(key string, ttl time.Duration, rdb *redis.Client) *jsonCache {
	return &jsonCache{key: key, ttl: ttl, rdb: rdb}
}

// Get returns the cached bytes when a fresh value exists. A nil cache is a
// no-op so handlers that run in tests (without Register) keep working.
func (c *jsonCache) Get(ctx context.Context) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	if c.rdb != nil {
		redisCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		v, err := c.rdb.Get(redisCtx, c.key).Bytes()
		cancel()
		if err == nil && len(v) > 0 {
			return v, true
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.value != nil && time.Since(c.at) < c.ttl {
		return c.value, true
	}
	return nil, false
}

// Set writes the value to memory immediately and to Redis when configured.
// Redis errors are ignored: the in-memory layer still serves this process.
func (c *jsonCache) Set(ctx context.Context, value []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.value = append([]byte(nil), value...)
	c.at = time.Now()
	c.mu.Unlock()
	if c.rdb != nil {
		redisCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_ = c.rdb.Set(redisCtx, c.key, value, c.ttl).Err()
		cancel()
	}
}
