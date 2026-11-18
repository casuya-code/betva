package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// youtubeIDRe matches a canonical 11-character YouTube video ID.
var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youtubeChannelURL returns the admin-configured YouTube channel URL used by
// the subscribe button on YouTube posts. Empty when unset (button hidden).
func (s *Server) youtubeChannelURL(ctx context.Context) string {
	if s.Pool == nil {
		return ""
	}
	var v string
	if err := s.Pool.QueryRow(ctx, `SELECT value FROM site_settings WHERE key = 'youtube_channel_url'`).Scan(&v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
// URL shape: youtu.be/<id>, youtube.com/watch?v=<id>, /shorts/<id>,
// /embed/<id>, /live/<id>, with or without www/m./scheme and extra query
// params. ok is false for anything that does not resolve to a valid ID, so
// only an ID — never a URL — reaches the database.
func extractYouTubeID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	// A bare ID pasted directly.
	if youtubeIDRe.MatchString(raw) {
		return raw, true
	}
	// Normalize so url.Parse sees a host even without a scheme.
	if !strings.Contains(raw, "://") {
		raw = "https://" + strings.TrimPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	host = strings.TrimPrefix(host, "music.")

	var id string
	switch {
	case host == "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
	case host == "youtube.com" || host == "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/shorts/"),
			strings.HasPrefix(u.Path, "/embed/"),
			strings.HasPrefix(u.Path, "/live/"),
			strings.HasPrefix(u.Path, "/v/"):
			rest := u.Path
			for _, p := range []string{"/shorts/", "/embed/", "/live/", "/v/"} {
				if strings.HasPrefix(rest, p) {
					rest = strings.TrimPrefix(rest, p)
					break
				}
			}
			if i := strings.Index(rest, "/"); i >= 0 {
				rest = rest[:i]
			}
			id = rest
		}
	default:
		return "", false
	}
	if !youtubeIDRe.MatchString(id) {
		return "", false
	}
	return id, true
}
// handleAdminSetYouTubeChannel stores the YouTube channel URL used by the
// subscribe button on admin-published YouTube posts. Empty clears it.
func (s *Server) handleAdminSetYouTubeChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	if body.URL != "" &&
		!strings.Contains(body.URL, "youtube.com") && !strings.Contains(body.URL, "youtu.be") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "must be a YouTube channel URL"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO site_settings (key, value, updated_at)
		VALUES ('youtube_channel_url', $1, NOW())
		ON CONFLICT (key) DO UPDATE
		    SET value = EXCLUDED.value, updated_at = NOW()
	`, body.URL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save setting"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"youtube_channel_url": body.URL})
}
