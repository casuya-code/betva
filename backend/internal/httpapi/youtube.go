package httpapi

import (
	"net/url"
	"regexp"
	"strings"
)

// youtubeIDRe matches a canonical 11-character YouTube video ID.
var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// extractYouTubeID pulls the 11-character video ID out of any common YouTube
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
