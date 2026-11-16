package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter is a fixed-window counter keyed by an arbitrary string (client
// IP, phone number, or user ID). It is in-memory and per-process: with one API
// replica that is the whole picture. If the API is ever scaled out, move the
// counters to Redis so limits are shared.
type RateLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	buckets   map[string]*rateBucket
	lastSweep time.Time
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
		ok, retry := l.Allow(keyFn(r))
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
