package httpapi

import (
	"strings"
	"testing"
)

func TestParseOddsBP(t *testing.T) {
	cases := []struct {
		in      string
		wantBP  int
		present bool
		wantErr bool
	}{
		{"", 0, false, false},
		{"1.50", 15000, true, false},
		{"2", 20000, true, false},
		{" 1.75 ", 17500, true, false},
		{"1.00", 0, true, true},
		{"0.9", 0, true, true},
		{"abc", 0, true, true},
	}
	for _, tc := range cases {
		bp, present, err := parseOddsBP(tc.in)
		if bp != tc.wantBP || present != tc.present || (err != nil) != tc.wantErr {
			t.Errorf("parseOddsBP(%q) = %d, %v, %v; want %d, %v, err=%v",
				tc.in, bp, present, err, tc.wantBP, tc.present, tc.wantErr)
		}
	}
}

func TestParseMatchesCSVDateAndOdds(t *testing.T) {
	csvData := `kickoff,date,league,home_team,away_team,markets,ft_odds,ht_odds,confidence
16:00,2026-11-16,Liga 1,Team A,Team B,ft_over_0_5|ht_over_0_5,1.50,2.10,HIGH
18:30,2026-11-17,Liga 2,Team C,Team D,ft_over_0_5,1.80,,MEDIUM
19:00,2026-11-16,Liga 1,Team A,Team B,ft_over_0_5,1.50,,LOW
20:00,16-11-2026,Bad Date,Team E,Team F,ft_over_0_5,1.50,,LOW
21:00,2026-11-16,Bad Odds,Team G,Team H,ft_over_0_5,0.5,,LOW`

	good, errs, err := parseMatchesCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(good) != 2 {
		t.Fatalf("want 2 valid rows, got %d (%+v)", len(good), good)
	}
	if len(errs) != 3 {
		t.Fatalf("want 3 row errors (bad date, duplicate, bad odds), got %d: %+v", len(errs), errs)
	}
	if good[0].Date != "2026-11-16" || good[1].Date != "2026-11-17" {
		t.Errorf("dates not preserved per row: %q, %q", good[0].Date, good[1].Date)
	}
	if good[0].OddsBP[marketFTOver05] != 15000 || good[0].OddsBP[marketHTOver05] != 21000 {
		t.Errorf("FT/HT odds not parsed: %+v", good[0].OddsBP)
	}
	if good[1].OddsBP[marketFTOver05] != 18000 {
		t.Errorf("FT odds not parsed: %+v", good[1].OddsBP)
	}
	if _, ok := good[1].OddsBP[marketHTOver05]; ok {
		t.Error("empty ht_odds should not set a bp value")
	}
}

func TestParseMatchesCSVDuplicateIncludesDate(t *testing.T) {
	csvData := `kickoff,date,home_team,away_team,markets,ft_odds
16:00,2026-11-16,Team A,Team B,ft_over_0_5,1.50
18:00,2026-11-17,Team A,Team B,ft_over_0_5,1.50`
	good, errs, err := parseMatchesCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(good) != 2 || len(errs) != 0 {
		t.Fatalf("same fixture on different dates must both be valid: good=%d errs=%+v", len(good), errs)
	}
}
