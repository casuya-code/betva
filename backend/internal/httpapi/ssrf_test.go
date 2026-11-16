package httpapi

import (
	"context"
	"net"
	"net/url"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"100.64.0.1", false},
		{"0.0.0.0", false},
		{"224.0.0.1", false},
		{"::1", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"2001:4860:4860::8888", true},
	}
	for _, c := range cases {
		if got := isPublicIP(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("isPublicIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestPublicHTTPClientRejectsPrivateTargets(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080/admin",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"http://10.0.0.5/",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publicHTTPClient(context.Background(), u); err == nil {
			t.Errorf("%s should be rejected", raw)
		}
	}
}

func TestValidProfilePic(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"/media/11fd479d0cd468144f79cb39.jpg", true},
		{"/media/a_b-c.webp", true},
		{"", false},
		{"javascript:alert(1)", false},
		{"data:image/svg+xml,<svg>", false},
		{"https://evil.example/x.jpg", false},
		{"/media/../etc/passwd.jpg", false},
		{"/media/x.svg", false},
		{"/media/x.png?x=1", false},
	} {
		if got := validProfilePic(tc.value); got != tc.want {
			t.Errorf("validProfilePic(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
