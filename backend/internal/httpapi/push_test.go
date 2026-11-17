package httpapi

import (
	"testing"
	"time"

	"github.com/betva/backend/internal/config"
)

func TestQuietNowOvernight(t *testing.T) {
	s := testServer()
	cases := []struct {
		hour int
		want bool
	}{
		{6, true},
		{7, false},
		{12, false},
		{21, false},
		{22, true},
		{23, true},
	}
	for _, tc := range cases {
		now := time.Date(2026, 11, 17, tc.hour, 0, 0, 0, eatLocation)
		if got := s.quietNow(now); got != tc.want {
			t.Errorf("quietNow(%02d:00) = %v, want %v", tc.hour, got, tc.want)
		}
	}
}

func TestQuietNowDaytimeWindow(t *testing.T) {
	s := testServer()
	s.Cfg.QuietStartHour = 9
	s.Cfg.QuietEndHour = 17
	for hour, want := range map[int]bool{8: false, 9: true, 16: true, 17: false, 22: false} {
		now := time.Date(2026, 11, 17, hour, 0, 0, 0, eatLocation)
		if got := s.quietNow(now); got != want {
			t.Errorf("quietNow(%02d:00) = %v, want %v", hour, got, want)
		}
	}
}

func TestPushBackoffIncreases(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, d := range want {
		if got := pushBackoff(i + 1); got != d {
			t.Errorf("pushBackoff(%d) = %v, want %v", i+1, got, d)
		}
	}
}

func TestPushDeferUntil(t *testing.T) {
	s := testServer()
	// 23:30 EAT on 17 Nov: quiet hours end at 07:01 the next morning.
	now := time.Date(2026, 11, 17, 23, 30, 0, 0, eatLocation)
	next := s.pushDeferUntil(now, "quiet hours")
	if next.Day() != 18 || next.Hour() != 7 || next.Minute() != 1 {
		t.Fatalf("quiet-hours retry = %v, want 18 Nov 07:01 EAT", next)
	}

	// A capped user becomes eligible one minute into the next EAT day.
	capNext := s.pushDeferUntil(now, "daily cap reached")
	if capNext.Day() != 18 || capNext.Hour() != 0 || capNext.Minute() != 1 {
		t.Fatalf("daily-cap retry = %v, want 18 Nov 00:01 EAT", capNext)
	}
}

func TestPushDisabledWhenKeysMissing(t *testing.T) {
	c := &config.Config{VAPIDPublicKey: "only-public"}
	if c.PushEnabled() {
		t.Fatal("push must stay off without a private key")
	}
}
