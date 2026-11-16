package main

import (
	"testing"
	"time"

	"github.com/betva/backend/internal/config"
)

func testCfg() *config.BackupConfig {
	return &config.BackupConfig{DailyKeepDays: 14, WeeklyKeepDays: 56, MonthlyKeepDays: 183}
}

func TestKeyTime(t *testing.T) {
	got, ok := keyTime("db/2026/11/16/betva-20261116T010000Z.dump")
	if !ok {
		t.Fatal("expected key to parse")
	}
	want := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	if _, ok := keyTime("db/not-a-dump.dump"); ok {
		t.Fatal("malformed key should not parse")
	}
}

func TestKeepDump(t *testing.T) {
	now := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	cfg := testCfg()

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"today", now, true},
		{"yesterday", now.Add(-23 * time.Hour), true},
		{"2 days old", now.Add(-48 * time.Hour), true},
		{"exactly 14 days old", now.Add(-14 * 24 * time.Hour), true},
		{"17 days old Friday", time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC), false},
		{"15 days old Sunday", time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), true},
		{"22 days old Sunday", time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC), true},
		{"43 days old Sunday", time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC), true},
		{"64 days old Sunday", time.Date(2026, 9, 13, 1, 0, 0, 0, time.UTC), false},
		{"107 days old first of month", time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC), true},
		{"100 days old mid month", time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC), false},
		{"229 days old first of month", time.Date(2026, 4, 1, 1, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		if got := keepDump(c.when, now, cfg); got != c.want {
			t.Errorf("%s (%s): got keep=%v, want %v", c.name, c.when.Format("2006-01-02 Mon"), got, c.want)
		}
	}
}

func TestDatabaseHost(t *testing.T) {
	got := databaseHost("postgresql://postgres:secret@postgres.railway.internal:5432/railway")
	if got != "postgres.railway.internal:5432" {
		t.Fatalf("got %q", got)
	}
	if got := databaseHost("no-at-sign"); got != "unknown" {
		t.Fatalf("got %q", got)
	}
}
