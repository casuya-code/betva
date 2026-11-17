package httpapi

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter is a fixed-window counter keyed by an arbitrary string (client
// IP, phone number, or user ID). By default it is in-memory and per-process.
// NewSharedRateLimiter adds a Redis layer so counters are shared across API
// replicas; the in-memory map is kept as a fast path and as a fallback when
// Redis is down, so a cache outage can never turn into an open gate.
type RateLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	buckets   map[string]*rateBucket
	lastSweep time.Time

	rdb    *redis.Client
	prefix string
}

type rateBucket struct {
	count int
	reset time.Time
}

// NewRateLimiter allows max requests per key in each window. max <= 0 disables
// the limiter, which keeps tests and local development simple.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		max:       max,
		window:    window,
		buckets:   make(map[string]*rateBucket),
		lastSweep: time.Now(),
	}
}

// NewSharedRateLimiter builds a limiter that also counts in Redis under the
// given key prefix. A nil rdb is fine: the limiter then behaves exactly like
// NewRateLimiter. Redis keys are namespaced and expire with the window.
func NewSharedRateLimiter(max int, window time.Duration, rdb *redis.Client, prefix string) *RateLimiter {
	l := NewRateLimiter(max, window)
	l.rdb = rdb
	l.prefix = prefix
	return l
}

// rateLimitScript increments the window counter atomically and returns the
// new count and the remaining TTL in milliseconds. PEXPIRE is only set on the
// first hit so the window starts with the first request, not the last.
const rateLimitScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
return { count, ttl }
`

// Allow reports whether key may proceed and, when it may not, how long until
// the current window resets. A nil limiter always allows.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	if l == nil || l.max <= 0 {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) > l.window {
		for k, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}

	b := l.buckets[key]
	if b == nil || now.After(b.reset) {
		b = &rateBucket{reset: now.Add(l.window)}
		l.buckets[key] = b
	}
	if b.count >= l.max {
		return false, time.Until(b.reset)
	}
	b.count++
	return true, 0
}

// AllowCtx is Allow with a shared Redis counter. On Redis errors it falls back
// to the in-memory counter, which keeps single-replica behaviour intact and
// fails open only to the same local limit rather than removing the cap.
func (l *RateLimiter) AllowCtx(ctx context.Context, key string) (bool, time.Duration) {
	if l == nil || l.max <= 0 {
		return true, 0
	}
	if l.rdb != nil {
		// A slow or dead Redis must never stall a request. Bound the call; on
		// timeout we fall through to the local counter immediately.
		redisCtx, cancel := context.WithTimeout(ctx, 75*time.Millisecond)
		defer cancel()
		redisKey := "betva:rl:" + l.prefix + ":" + key
		res, err := l.rdb.Eval(redisCtx, rateLimitScript, []string{redisKey}, l.window.Milliseconds()).Result()
		if err == nil {
			vals, ok := res.([]interface{})
			if ok && len(vals) == 2 {
				count, _ := vals[0].(int64)
				ttlMS, _ := vals[1].(int64)
				if ttlMS < 0 {
					ttlMS = l.window.Milliseconds()
				}
				if count > int64(l.max) {
					return false, time.Duration(ttlMS) * time.Millisecond
				}
				return true, 0
			}
		}
	}
	return l.Allow(key)
}

// Middleware limits requests by client IP. It runs after chi's RealIP
// middleware, so RemoteAddr already reflects X-Forwarded-For from Railway.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return l.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	}))
}

// Wrap limits a single handler by client IP.
func (l *RateLimiter) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return l.wrapKey(next, clientIP)
}

// WrapUser limits a protected handler by authenticated user ID, falling back
// to the client IP. Keying by user avoids punishing everyone behind the same
// mobile-carrier NAT.
func (l *RateLimiter) WrapUser(next http.HandlerFunc) http.HandlerFunc {
	return l.wrapKey(next, func(r *http.Request) string {
		if u := UserFromCtx(r); u != nil && u.ID != "" {
			return "user:" + u.ID
		}
		return clientIP(r)
	})
}

func (l *RateLimiter) wrapKey(next http.HandlerFunc, keyFn func(*http.Request) string) http.HandlerFunc {
	if l == nil || l.max <= 0 {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ok, retry := l.AllowCtx(r.Context(), keyFn(r))
		if !ok {
			writeRateLimited(w, retry)
			return
		}
		next(w, r)
	}
}

// writeRateLimited sends a 429 with a Retry-After hint.
func writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{
		"error": "too many requests, try again later",
	})
}

// clientIP returns the address RealIP left in RemoteAddr, without the port.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	return r.RemoteAddr
}
