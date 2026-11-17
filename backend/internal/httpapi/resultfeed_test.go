package httpapi

import (
	"encoding/json"
	"testing"
)

func TestFoldTeamName(t *testing.T) {
	cases := [][2]string{
		{"Côte d'Ivoire U23", "cote divoire u23"},
		{"Manchester FC", "manchester"},
		{"  São Paulo U20  ", "sao paulo u20"},
		{"Atlético Mineiro U20", "atletico mineiro u20"},
		{"Sint Maarten", "sint maarten"},
	}
	for _, c := range cases {
		if got := foldTeamName(c[0]); got != c[1] {
			t.Errorf("foldTeamName(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func testESPNBoard(t *testing.T, raw string) *espnScoreboard {
	t.Helper()
	var board espnScoreboard
	if err := json.Unmarshal([]byte(raw), &board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	return &board
}

func intp(n int) *int { return &n }

func TestFindESPNResult_FullAndHalfTime(t *testing.T) {
	board := testESPNBoard(t, `{
		"events": [
			{"id":"1","date":"2026-11-16T19:00Z","name":"Ajax at Sporting","competitions":[{
				"status":{"type":{"name":"STATUS_SCHEDULED","state":"pre","completed":false}},
				"competitors":[
					{"homeAway":"home","score":"0","team":{"id":"1","displayName":"Ajax"}},
					{"homeAway":"away","score":"0","team":{"id":"2","displayName":"Sporting"}}
				]
			}]},
			{"id":"2","date":"2026-11-16T19:00Z","name":"Ajax at Sporting","competitions":[{
				"status":{"type":{"name":"STATUS_FULL_TIME","state":"post","completed":true,"description":"Full Time"}},
				"competitors":[
					{"homeAway":"home","score":"3","team":{"id":"1","displayName":"Ajax"}},
					{"homeAway":"away","score":"2","team":{"id":"2","displayName":"Sporting CP","shortDisplayName":"Sporting"}}
				],
				"details":[
					{"type":{"text":"Goal"},"clock":{"displayValue":"5'"},"team":{"id":"1"},"scoringPlay":true},
					{"type":{"text":"Goal - Free-kick"},"clock":{"displayValue":"40'"},"team":{"id":"1"},"scoringPlay":true},
					{"type":{"text":"Goal"},"clock":{"displayValue":"49'"},"team":{"id":"2"},"scoringPlay":true},
					{"type":{"text":"Goal"},"clock":{"displayValue":"71'"},"team":{"id":"2"},"scoringPlay":true},
					{"type":{"text":"Goal"},"clock":{"displayValue":"102'"},"team":{"id":"1"},"scoringPlay":true},
					{"type":{"text":"Yellow Card"},"clock":{"displayValue":"60'"},"team":{"id":"1"}}
				]
			}]}
		]
	}`)

	got := findESPNResult(board, "Ajax FC", "Sporting", "https://example.test/board")
	if got == nil {
		t.Fatal("expected a result")
	}
	if got.HomeGoals != 3 || got.AwayGoals != 2 {
		t.Fatalf("FT = %d-%d, want 3-2", got.HomeGoals, got.AwayGoals)
	}
	if got.HomeGoalsHT == nil || *got.HomeGoalsHT != 2 || got.AwayGoalsHT == nil || *got.AwayGoalsHT != 0 {
		t.Fatalf("HT = %v-%v, want 2-0", intPtrStr(got.HomeGoalsHT), intPtrStr(got.AwayGoalsHT))
	}
	if got.Source != "espn" || got.SourceURL == "" {
		t.Fatalf("source not recorded: %+v", got)
	}
}

func TestFindESPNResult_IgnoresUnfinishedAndWrongTeams(t *testing.T) {
	board := testESPNBoard(t, `{"events":[
		{"id":"1","competitions":[{"status":{"type":{"state":"in","completed":false}},
		 "competitors":[{"homeAway":"home","score":"1","team":{"displayName":"Ajax"}},
		                {"homeAway":"away","score":"0","team":{"displayName":"Sporting"}}]}]},
		{"id":"2","competitions":[{"status":{"type":{"state":"post","completed":true}},
		 "competitors":[{"homeAway":"home","score":"1","team":{"displayName":"Ajax"}},
		                {"homeAway":"away","score":"0","team":{"displayName":"Different FC"}}]}]}
	]}`)
	if got := findESPNResult(board, "Ajax", "Sporting", "u"); got != nil {
		t.Fatalf("must not use live score or wrong fixture: %+v", got)
	}
}

func TestDeriveHalfTime_Safety(t *testing.T) {
	empty := espnCompetition{}
	h, a := deriveHalfTime(empty, "1", "2", 0, 0)
	if h == nil || a == nil || *h != 0 || *a != 0 {
		t.Fatal("0-0 full time must give a certain 0-0 half time")
	}
	h, a = deriveHalfTime(empty, "1", "2", 2, 0)
	if h != nil || a != nil {
		t.Fatal("missing goal details must not produce a guessed half time")
	}
}

func TestParse100Tip(t *testing.T) {
	page := `<details class="mr" data-id="1" data-mk="fh" data-c="FU1.5" data-b="{&quot;n&quot;:[&quot;Kosovo U21&quot;,&quot;Draw&quot;,&quot;Luxembourg U21&quot;],&quot;f&quot;:&quot;Expected goals 1.5 (1.2 - 0.3) · Half-time 0-1&quot;}">
  <summary><span class="mr-teams">...</span>
  <span class="mr-sc"><b>0</b><b>2</b></span></summary></details>`
	rows := parse100Tip(page)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Home != "Kosovo U21" || r.Away != "Luxembourg U21" || r.FTHome != 0 || r.FTAway != 2 {
		t.Fatalf("bad FT row: %+v", r)
	}
	if r.HTHome == nil || *r.HTHome != 0 || r.HTAway == nil || *r.HTAway != 1 {
		t.Fatalf("bad HT row: %+v", r)
	}

	res := &fetchedResult{HomeGoals: 0, AwayGoals: 2, HomeGoalsHT: intp(0), AwayGoalsHT: intp(1)}
	if tipConflicts(rows, res, "Kosovo U21", "Luxembourg U21") {
		t.Fatal("matching scores must not conflict")
	}
	res.AwayGoals = 3
	if !tipConflicts(rows, res, "Kosovo U21", "Luxembourg U21") {
		t.Fatal("different scores must block auto-approval")
	}
	if tipConflicts(rows, res, "Other", "Fixture") {
		t.Fatal("a different fixture is not a conflict")
	}
}
