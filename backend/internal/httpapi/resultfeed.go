package httpapi

// Deterministic result discovery from public websites. This is deliberately
// separate from the Groq AI assistant: no model, no prompt, no inference.
// Scores come only from structured data on the page (ESPN scoreboard JSON,
// 100Tip graded rows) and are matched to Betva fixtures by folded team name
// and kickoff date. A discovered score is stored as an APPROVED suggestion so
// the existing settlement sweep settles it; the raw feed can never settle a
// slip on its own without passing through that audited row.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

const (
	resultFeedUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	resultFeedReadCap  = 4 << 20 // ESPN scoreboards can be ~650 KB on busy days
	espnScoreboardURL  = "https://site.api.espn.com/apis/site/v2/sports/soccer/all/scoreboard?dates=%s"
	tip100TodayURL     = "https://www.100tip.com/first-half-goals"
	tip100YesterdayURL = "https://www.100tip.com/first-half-goals/yesterday"
)

// settlementCandidate is the row the settlement sweep and the web-result
// fetcher pass between them. Extra team/date fields exist only so the fetcher
// can match a fixture against an external feed.
type settlementCandidate struct {
	id        string
	status    string
	home      *int
	away      *int
	homeHT    *int
	awayHT    *int
	homeTeam  string
	awayTeam  string
	matchDate time.Time
}

// ---------------------------------------------------------------------------
// Fetching

// fetchPublicURL reuses the SSRF-hardened client from ssrf.go. A caller-
// supplied URL still needs the scheme check, 2xx check, read cap and browser
// UA; those live here so both the AI flow and this one behave the same way.
func fetchPublicURL(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid result source URL %q", rawURL)
	}
	client, err := publicHTTPClient(ctx, u)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", resultFeedUA)
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u.Host)
	}
	return io.ReadAll(io.LimitReader(resp.Body, resultFeedReadCap))
}

// ---------------------------------------------------------------------------
// Team-name folding

// accentFold covers the Latin characters seen in football team names. We keep
// it dependency-free (x/text is only an indirect module here) and conservative:
// unknown runes are dropped unless they are letters, digits or spaces.
var accentFold = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'æ': "ae", 'ç': "c", 'ć': "c", 'ĉ': "c", 'č': "c", 'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ğ': "g", 'ĝ': "g", 'ġ': "g", 'ģ': "g", 'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i",
	'ĵ': "j", 'ķ': "k", 'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ł': "l", 'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o", 'œ': "oe",
	'ŕ': "r", 'ŗ': "r", 'ř': "r", 'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s",
	'ţ': "t", 'ť': "t", 'ŧ': "t", 'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w", 'ý': "y", 'ÿ': "y", 'ŷ': "y", 'ź': "z", 'ż': "z", 'ž': "z", 'ß': "ss",
}

// foldTeamName makes two feeds comparable: lowercase, fold accents, drop
// punctuation, collapse whitespace, and trim the club suffixes feeds disagree
// on. It intentionally keeps age groups (U19/U21) and "B" teams: those are
// different fixtures and must not be cross-matched.
func foldTeamName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if repl, ok := accentFold[r]; ok {
			b.WriteString(repl)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
			continue
		}
		// Separators used in team names differ between feeds: Betva writes
		// "Bosnia & Herzegovina", ESPN "Bosnia-Herzegovina". Normalise them
		// to spaces so both fold to "bosnia herzegovina". Apostrophes are
		// dropped instead, so "Côte d'Ivoire" stays "cote divoire".
		switch r {
		case '-', '–', '—', '/', '&', '.':
			b.WriteByte(' ')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	for _, suffix := range []string{" football club", " athletic club", " fc", " afc", " sc", " cf", " ac", " club"} {
		out = strings.TrimSuffix(out, suffix)
	}
	return out
}

// ---------------------------------------------------------------------------
// ESPN scoreboard

type espnScoreboard struct {
	Events []espnEvent `json:"events"`
}

type espnEvent struct {
	ID           string            `json:"id"`
	Date         string            `json:"date"`
	Name         string            `json:"name"`
	Competitions []espnCompetition `json:"competitions"`
}

type espnCompetition struct {
	Status      espnStatus       `json:"status"`
	Competitors []espnCompetitor `json:"competitors"`
	Details     []espnDetail     `json:"details"`
}

type espnStatus struct {
	Type espnStatusType `json:"type"`
}

type espnStatusType struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Completed   bool   `json:"completed"`
	Description string `json:"description"`
}

type espnCompetitor struct {
	HomeAway string          `json:"homeAway"`
	Score    json.RawMessage `json:"score"`
	Team     struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		DisplayName      string `json:"displayName"`
		ShortDisplayName string `json:"shortDisplayName"`
		Abbreviation     string `json:"abbreviation"`
	} `json:"team"`
}

type espnDetail struct {
	Type struct {
		Text string `json:"text"`
	} `json:"type"`
	Clock struct {
		Value        float64 `json:"value"`
		DisplayValue string  `json:"displayValue"`
	} `json:"clock"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	ScoringPlay bool `json:"scoringPlay"`
	OwnGoal     bool `json:"ownGoal"`
	Shootout    bool `json:"shootout"`
}

// fetchedResult is the deterministic answer we are willing to approve.
type fetchedResult struct {
	HomeGoals   int    `json:"home_goals"`
	AwayGoals   int    `json:"away_goals"`
	HomeGoalsHT *int   `json:"home_goals_ht,omitempty"`
	AwayGoalsHT *int   `json:"away_goals_ht,omitempty"`
	Source      string `json:"source"`
	SourceURL   string `json:"source_url"`
	Detail      string `json:"detail"`
}

func parseESPNScore(raw json.RawMessage) (int, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		return n, err == nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), true
	}
	return 0, false
}

var leadingMinuteRe = regexp.MustCompile(`^\s*(\d+)`)

// minuteOf ESPN clocks look like "5'", "45+2'" or "102'". The first number is
// the minute of the period; first-half goals are <= 45.
func minuteOf(display string) int {
	m := leadingMinuteRe.FindStringSubmatch(display)
	if len(m) < 2 {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

func isGoalDetail(d espnDetail) bool {
	return d.ScoringPlay || strings.HasPrefix(strings.ToLower(d.Type.Text), "goal")
}

// deriveHalfTime counts first-half goals from ESPN's event details. It returns
// nil when the details are incomplete, so a half-time market is never graded
// from a guess. A 0-0 full-time score is the one safe exception: half-time must
// also have been 0-0.
func deriveHalfTime(comp espnCompetition, homeID, awayID string, ftHome, ftAway int) (*int, *int) {
	if len(comp.Details) == 0 {
		if ftHome == 0 && ftAway == 0 {
			zero := 0
			return &zero, &zero
		}
		return nil, nil
	}
	homeHT, awayHT, counted := 0, 0, 0
	for _, d := range comp.Details {
		if !isGoalDetail(d) || d.Shootout {
			continue
		}
		minute := minuteOf(d.Clock.DisplayValue)
		if minute < 0 {
			return nil, nil
		}
		teamID := d.Team.ID
		if d.OwnGoal {
			switch teamID {
			case homeID:
				teamID = awayID
			case awayID:
				teamID = homeID
			}
		}
		counted++
		if minute <= 45 {
			switch teamID {
			case homeID:
				homeHT++
			case awayID:
				awayHT++
			}
		}
	}
	if counted != ftHome+ftAway {
		return nil, nil // incomplete goal feed: do not guess half-time
	}
	return &homeHT, &awayHT
}

func espnTeamMatches(c espnCompetitor, folded string) bool {
	for _, n := range []string{c.Team.DisplayName, c.Team.ShortDisplayName, c.Team.Name, c.Team.Abbreviation} {
		if n != "" && foldTeamName(n) == folded {
			return true
		}
	}
	return false
}

func findESPNResult(sb *espnScoreboard, homeTeam, awayTeam, sourceURL string) *fetchedResult {
	homeKey, awayKey := foldTeamName(homeTeam), foldTeamName(awayTeam)
	if homeKey == "" || awayKey == "" {
		return nil
	}
	for i := range sb.Events {
		ev := &sb.Events[i]
		if len(ev.Competitions) == 0 {
			continue
		}
		comp := &ev.Competitions[0]
		if !comp.Status.Type.Completed || comp.Status.Type.State != "post" {
			continue
		}
		var home, away *espnCompetitor
		for j := range comp.Competitors {
			c := &comp.Competitors[j]
			switch c.HomeAway {
			case "home":
				home = c
			case "away":
				away = c
			}
		}
		if home == nil || away == nil || !espnTeamMatches(*home, homeKey) || !espnTeamMatches(*away, awayKey) {
			continue
		}
		hg, okH := parseESPNScore(home.Score)
		ag, okA := parseESPNScore(away.Score)
		if !okH || !okA || hg < 0 || ag < 0 || hg > 50 || ag > 50 {
			continue
		}
		res := &fetchedResult{
			HomeGoals: hg,
			AwayGoals: ag,
			Source:    "espn",
			SourceURL: sourceURL,
			Detail:    fmt.Sprintf("%s — ESPN %d-%d", ev.Name, hg, ag),
		}
		res.HomeGoalsHT, res.AwayGoalsHT = deriveHalfTime(*comp, home.Team.ID, away.Team.ID, hg, ag)
		return res
	}
	return nil
}

// ---------------------------------------------------------------------------
// 100Tip cross-check

type tipRowResult struct {
	Home   string
	Away   string
	FTHome int
	FTAway int
	HTHome *int
	HTAway *int
}

var (
	tipDetailsRe = regexp.MustCompile(`<details\b[^>]*\bdata-b="([^"]*)"`)
	tipScoreRe   = regexp.MustCompile(`(?s)<span class="mr-sc">\s*<b>(\d+)</b>\s*<b>(\d+)</b>\s*</span>`)
	tipHalfRe    = regexp.MustCompile(`(?i)half-time\s+(\d+)\s*[-–—]\s*(\d+)`)
)

type tipDataJSON struct {
	N []string `json:"n"`
	F string   `json:"f"`
}

func parse100Tip(page string) []tipRowResult {
	matches := tipDetailsRe.FindAllStringSubmatchIndex(page, -1)
	out := make([]tipRowResult, 0, len(matches))
	for _, m := range matches {
		var data tipDataJSON
		raw := html.UnescapeString(page[m[2]:m[3]])
		if err := json.Unmarshal([]byte(raw), &data); err != nil || len(data.N) < 3 {
			continue
		}
		score := tipScoreRe.FindStringSubmatch(page[m[1]:])
		if len(score) < 3 {
			continue
		}
		hg, errH := strconv.Atoi(score[1])
		ag, errA := strconv.Atoi(score[2])
		if errH != nil || errA != nil {
			continue
		}
		row := tipRowResult{Home: data.N[0], Away: data.N[2], FTHome: hg, FTAway: ag}
		if ht := tipHalfRe.FindStringSubmatch(data.F); len(ht) >= 3 {
			h, e1 := strconv.Atoi(ht[1])
			a, e2 := strconv.Atoi(ht[2])
			if e1 == nil && e2 == nil {
				row.HTHome, row.HTAway = &h, &a
			}
		}
		out = append(out, row)
	}
	return out
}

// tipConflicts returns true only when 100Tip has the same fixture with a
// different score. A missing row is not a conflict — 100Tip does not cover
// every ESPN league. Disagreement blocks auto-approval so a human reviews it.
func tipConflicts(rows []tipRowResult, res *fetchedResult, homeTeam, awayTeam string) bool {
	homeKey, awayKey := foldTeamName(homeTeam), foldTeamName(awayTeam)
	for _, row := range rows {
		if foldTeamName(row.Home) != homeKey || foldTeamName(row.Away) != awayKey {
			continue
		}
		if row.FTHome != res.HomeGoals || row.FTAway != res.AwayGoals {
			return true
		}
		if row.HTHome != nil && res.HomeGoalsHT != nil &&
			(*row.HTHome != *res.HomeGoalsHT || *row.HTAway != *res.AwayGoalsHT) {
			return true
		}
		return false
	}
	return false
}

func tipURLForDate(date time.Time) string {
	today := time.Now().In(eatZone)
	if date.Year() == today.Year() && date.YearDay() == today.YearDay() {
		return tip100TodayURL
	}
	yesterday := today.AddDate(0, 0, -1)
	if date.Year() == yesterday.Year() && date.YearDay() == yesterday.YearDay() {
		return tip100YesterdayURL
	}
	return "" // 100Tip only exposes today and yesterday pages
}

// ---------------------------------------------------------------------------
// Orchestration

type resultFetchStats struct {
	DatesScanned       int `json:"dates_scanned"`
	ESPNFixtures       int `json:"espn_fixtures"`
	Approved           int `json:"approved"`
	Conflicts          int `json:"conflicts"`
	SourcesUnavailable int `json:"sources_unavailable"`
}

// fetchAndApproveWebResults discovers scores for unsettled matches and writes
// them as approved result suggestions. It never touches matches, bets or
// wallets directly; the settlement sweep does that, with its normal locks.
func (s *Server) fetchAndApproveWebResults(ctx context.Context, cands []settlementCandidate) resultFetchStats {
	var stats resultFetchStats
	if !s.Cfg.AutoResultFetchEnabled || len(cands) == 0 {
		return stats
	}

	byDate := map[string][]settlementCandidate{}
	for _, c := range cands {
		if c.home != nil && c.away != nil {
			continue // admin-supplied score already wins
		}
		if c.status == "postponed" || c.status == "cancelled" {
			continue
		}
		key := c.matchDate.In(eatZone).Format("2006-01-02")
		byDate[key] = append(byDate[key], c)
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates))) // most recent first
	if max := s.Cfg.AutoResultFetchMaxDates; max > 0 && len(dates) > max {
		dates = dates[:max]
	}

	for _, date := range dates {
		if err := ctx.Err(); err != nil {
			break
		}
		stats.DatesScanned++

		compact := strings.ReplaceAll(date, "-", "")
		espnURL := fmt.Sprintf(espnScoreboardURL, compact)
		espnRaw, err := fetchPublicURL(ctx, espnURL)
		if err != nil {
			log.Printf("auto-result: ESPN %s: %v", date, err)
			stats.SourcesUnavailable++
			continue
		}
		var board espnScoreboard
		if err := json.Unmarshal(espnRaw, &board); err != nil {
			log.Printf("auto-result: ESPN %s decode: %v", date, err)
			stats.SourcesUnavailable++
			continue
		}

		// ESPN groups fixtures by UTC date, while Betva stores the East Africa
		// date. A late-evening EAT kickoff can land on the previous UTC day, so
		// check the neighbouring scoreboards too — but only if the primary one
		// did not produce a result for that fixture.
		adjacent := map[string]*espnScoreboard{}
		findAcrossDates := func(home, away string) *fetchedResult {
			if res := findESPNResult(&board, home, away, espnURL); res != nil {
				return res
			}
			base := mustParseFeedDate(date)
			for _, offset := range []int{-1, 1} {
				adjDate := base.AddDate(0, 0, offset).Format("20060102")
				adjURL := fmt.Sprintf(espnScoreboardURL, adjDate)
				adjBoard, ok := adjacent[adjDate]
				if !ok {
					adjRaw, err := fetchPublicURL(ctx, adjURL)
					if err != nil {
						adjacent[adjDate] = nil
						log.Printf("auto-result: ESPN adjacent %s: %v", adjDate, err)
						continue
					}
					var decoded espnScoreboard
					if err := json.Unmarshal(adjRaw, &decoded); err != nil {
						adjacent[adjDate] = nil
						continue
					}
					adjBoard = &decoded
					adjacent[adjDate] = adjBoard
				}
				if adjBoard != nil {
					if res := findESPNResult(adjBoard, home, away, adjURL); res != nil {
						return res
					}
				}
			}
			return nil
		}

		// Cross-check is best effort. A failure here does not discard ESPN.
		var tips []tipRowResult
		if tipURL := tipURLForDate(mustParseFeedDate(date)); tipURL != "" {
			if tipRaw, err := fetchPublicURL(ctx, tipURL); err != nil {
				log.Printf("auto-result: 100Tip %s: %v", date, err)
			} else {
				tips = parse100Tip(string(tipRaw))
			}
		}

		for _, c := range byDate[date] {
			res := findAcrossDates(c.homeTeam, c.awayTeam)
			if res == nil {
				continue
			}
			stats.ESPNFixtures++

			if tipConflicts(tips, res, c.homeTeam, c.awayTeam) {
				log.Printf("auto-result: %s (%s vs %s) ESPN %d-%d conflicts with 100Tip; leaving for admin",
					date, c.homeTeam, c.awayTeam, res.HomeGoals, res.AwayGoals)
				stats.Conflicts++
				continue
			}

			approved, err := s.storeApprovedWebResult(ctx, c.id, res)
			if err != nil {
				log.Printf("auto-result: store %s: %v", c.id, err)
				continue
			}
			if approved {
				stats.Approved++
				log.Printf("auto-result: approved %s %s vs %s: %d-%d (HT %v-%v) from %s",
					c.id, c.homeTeam, c.awayTeam, res.HomeGoals, res.AwayGoals,
					intPtrStr(res.HomeGoalsHT), intPtrStr(res.AwayGoalsHT), res.Source)
			}
		}
	}
	return stats
}

// handleAdminScanResults lets an admin trigger result discovery for one date
// without waiting for the next sweep. It only writes approved suggestions;
// settlement still happens through the normal guarded path.
func (s *Server) handleAdminScanResults(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date string `json:"date"`
	}
	if err := jsonDecode(r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	date := strings.TrimSpace(body.Date)
	if date == "" {
		date = time.Now().In(eatZone).Format("2006-01-02")
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, eatZone)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, status, home_goals, away_goals, home_goals_ht, away_goals_ht,
		       home_team, away_team, match_date
		FROM matches
		WHERE settled_at IS NULL AND match_date = $1
		  AND status NOT IN ('postponed', 'cancelled')
		ORDER BY kickoff_time
	`, parsed)
	if err != nil {
		log.Printf("auto-result: scan query: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan failed"})
		return
	}
	defer rows.Close()

	var cands []settlementCandidate
	for rows.Next() {
		var c settlementCandidate
		if err := rows.Scan(&c.id, &c.status, &c.home, &c.away, &c.homeHT, &c.awayHT,
			&c.homeTeam, &c.awayTeam, &c.matchDate); err != nil {
			log.Printf("auto-result: scan row: %v", err)
			continue
		}
		cands = append(cands, c)
	}

	stats := s.fetchAndApproveWebResults(r.Context(), cands)
	writeJSON(w, http.StatusOK, stats)
}

func intPtrStr(p *int) string {
	if p == nil {
		return "?"
	}
	return strconv.Itoa(*p)
}

func mustParseFeedDate(date string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", date, eatZone)
	if err != nil {
		return time.Now().In(eatZone)
	}
	return t
}

// storeApprovedWebResult inserts one audit row. The guard avoids re-approving
// the same match every sweep when settlement itself is temporarily failing.
func (s *Server) storeApprovedWebResult(ctx context.Context, matchID string, res *fetchedResult) (bool, error) {
	var exists int
	err := s.Pool.QueryRow(ctx, `
		SELECT 1 FROM result_suggestions
		WHERE match_id = $1 AND status = 'approved' AND provider IN ('espn', '100tip')
		LIMIT 1
	`, matchID).Scan(&exists)
	if err == nil {
		return false, nil
	}
	// pgx returns ErrNoRows when there is no prior automatic approval. Any
	// other error is real and must not be swallowed.
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	raw, _ := json.Marshal(res)
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO result_suggestions
		  (match_id, source_url, source_excerpt, suggested_home, suggested_away,
		   suggested_home_ht, suggested_away_ht, confidence, provider, model,
		   raw_response, status, reviewed_by, reviewed_at, review_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'high', $8, 'deterministic-v1', $9,
		        'approved', 'auto-web-result', NOW(), $10)
	`, matchID, res.SourceURL, res.Detail, res.HomeGoals, res.AwayGoals,
		res.HomeGoalsHT, res.AwayGoalsHT, res.Source, raw,
		"automatic web result (admin can correct via settlement)")
	if err != nil {
		return false, err
	}
	return true, nil
}
