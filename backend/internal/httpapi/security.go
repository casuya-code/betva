package httpapi

import (
	"net/http"
	"strings"
)

// SecurityHeaders applies baseline hardening headers to every API and media
// response. The frontend is served by Vercel, which sets its own headers in
// frontend/vercel.json; this covers the Go origin.
func SecurityHeaders(env string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			if env == "production" {
				// Railway terminates TLS and forwards the request over HTTP.
				// Browsers only honour HSTS when the response arrived over HTTPS.
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				// API responses are per-user and must never be cached by a proxy.
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}
