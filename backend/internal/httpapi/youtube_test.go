package httpapi

import "testing"

func TestExtractYouTubeID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "dQw4w9WgXcQ", true},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"  https://youtu.be/dQw4w9WgXcQ  ", "dQw4w9WgXcQ", true},
		// Rejected: wrong host, malformed ID, empty, javascript: scheme.
		{"https://evil.example/watch?v=dQw4w9WgXcQ", "", false},
		{"https://www.youtube.com/watch?v=short", "", false},
		{"https://www.youtube.com/watch", "", false},
		{"", "", false},
		{"javascript:alert(1)", "", false},
	}
	for _, c := range cases {
		got, ok := extractYouTubeID(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("extractYouTubeID(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
