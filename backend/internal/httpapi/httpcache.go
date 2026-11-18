package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// writeCachedBody sends a pre-serialised JSON body with a body-derived ETag.
// A client that already holds that exact body gets a 304 with no payload.
// Cache-Control is Set so it overrides the global API "no-store" default;
// Vary is Add so CORS' Origin and the compressor's Accept-Encoding survive.
func writeCachedBody(w http.ResponseWriter, r *http.Request, status int, cacheControl string, varyAuthorization bool, body []byte) {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`

	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", cacheControl)
	h.Set("ETag", etag)
	if varyAuthorization {
		h.Add("Vary", "Authorization")
	}

	if match := r.Header.Get("If-None-Match"); match != "" {
		for _, candidate := range strings.Split(match, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == etag || candidate == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}

	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeCachedJSON marshals once and delegates to writeCachedBody so the ETag
// always describes the exact bytes on the wire.
func writeCachedJSON(w http.ResponseWriter, r *http.Request, status int, cacheControl string, varyAuthorization bool, v interface{}) {
	body, err := json.Marshal(v)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode error"})
		return
	}
	writeCachedBody(w, r, status, cacheControl, varyAuthorization, body)
}
