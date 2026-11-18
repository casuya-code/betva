package httpapi

import (
	"context"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
	local := &mediaStore{}
	r2 := &mediaStore{r2Enabled: true, r2PublicBase: "https://pub.example.r2.dev"}
	r2Presign := &mediaStore{r2Enabled: true, r2Bucket: "bucket", r2Presigner: s3.NewPresignClient(s3.NewFromConfig(aws.Config{Region: "auto"})), r2PresignTTL: time.Hour}

	for _, tc := range []struct {
		value string
		ms    *mediaStore
		want  bool
	}{
		{"/media/11fd479d0cd468144f79cb39.jpg", local, true},
		{"/media/a_b-c.webp", local, true},
		{"", local, false},
		{"javascript:alert(1)", local, false},
		{"data:image/svg+xml,<svg>", local, false},
		{"https://evil.example/x.jpg", local, false},
		{"/media/../etc/passwd.jpg", local, false},
		{"/media/x.svg", local, false},
		{"/media/x.png?x=1", local, false},
		{"https://pub.example.r2.dev/media/11fd479d0cd468144f79cb39.jpg", r2, true},
		{"https://pub.example.r2.dev/media/11fd479d0cd468144f79cb39.mp4", r2, false},
		{"https://evil.example/media/11fd479d0cd468144f79cb39.jpg", r2, false},
		{"r2://bucket/media/11fd479d0cd468144f79cb39.jpg", r2Presign, true},
		{"r2://other/media/11fd479d0cd468144f79cb39.jpg", r2Presign, false},
	} {
		if got := validProfilePic(tc.value, tc.ms); got != tc.want {
			t.Errorf("validProfilePic(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
