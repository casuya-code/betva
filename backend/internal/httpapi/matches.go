package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- CSV contract -------------------------------------------------------
//
// Columns (header row required, order-independent):
//   date        YYYY-MM-DD — authoritative per row; one file may hold many days
//   kickoff     HH:MM (24h, EAT)
//   league      free text (optional)
//   home_team   required
//   away_team   required
//   markets     ft_over_0_5 and/or ht_over_0_5, separated by | or ,
//   ft_odds     decimal odds for the FT market, e.g. 1.50 (optional)
//   ht_odds     decimal odds for the HT market, e.g. 2.10 (optional)
//   confidence  HIGH | MEDIUM | LOW (optional, default MEDIUM)
//
// A row without a date falls back to the form field `date` (YYYY-MM-DD).
// A market without odds falls back to PAYOUT_MULTIPLIER_BP.

var (
	kickoffRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

const (
	marketFTOver05 = "ft_over_0_5"
	marketHTOver05 = "ht_over_0_5"
	maxCSVRows     = 500
	maxCSVBytes    = 2 << 20 // 2 MiB
)

type parsedMatch struct {
	Line       int            `json:"line"`
	Date       string         `json:"date"`
	Kickoff    string         `json:"kickoff"`
	League     string         `json:"league"`
	HomeTeam   string         `json:"home_team"`
	AwayTeam   string         `json:"away_team"`
	Markets    []string       `json:"markets"`
	OddsBP     map[string]int `json:"odds_bp"`
	Confidence string         `json:"confidence"`
}

type rowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func normalizeMarket(v string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	switch s {
	case "ft", "ft_over_0_5", "full_time_over_0_5", "fulltime_over_0_5", "over_0_5_ft":
		return marketFTOver05, true
	case "ht", "ht_over_0_5", "over_0_5_ht", "half_time_over_0_5", "halftime_over_0_5":
		return marketHTOver05, true
	}
	return "", false
}

func normalizeConfidence(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return "high"
	case "low":
		return "low"
	case "", "medium":
		return "medium"
	}
	return ""
}

// parseOddsBP converts a decimal odds cell ("1.50") into basis points
// (15000), the same unit used by bets.payout_multiplier_bp. Empty means the
// market carries no uploaded odds and the global multiplier applies.
func parseOddsBP(raw string) (bp int, present bool, err error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return 0, false, nil
	}
	f, parseErr := strconv.ParseFloat(v, 64)
	if parseErr != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, true, fmt.Errorf("must be a decimal number such as 1.50")
	}
	if f <= 1 || f > 1000 {
		return 0, true, fmt.Errorf("must be greater than 1.00 and at most 1000")
	}
	return int(math.Round(f * 10000)), true, nil
}

// parseMatchesCSV validates every row and returns the good rows plus a
// per-line error list. Bad rows are reported, never silently dropped.
func parseMatchesCSV(r io.Reader) ([]parsedMatch, []rowError, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read header: %w", err)
	}

	col := map[string]int{}
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		col[key] = i
	}

	required := []string{"kickoff", "home_team", "away_team", "markets"}
	for _, req := range required {
		if _, ok := col[req]; !ok {
			return nil, nil, fmt.Errorf("missing required column: %s", req)
		}
	}

	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	getAny := func(rec []string, names ...string) string {
		for _, name := range names {
			if v := get(rec, name); v != "" {
				return v
			}
		}
		return ""
	}

	var good []parsedMatch
	var errs []rowError
	seen := map[string]int{}

	line := 1
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			errs = append(errs, rowError{Line: line, Message: "malformed CSV row: " + err.Error()})
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if len(good)+len(errs) >= maxCSVRows {
			errs = append(errs, rowError{Line: line, Message: fmt.Sprintf("row limit reached (%d)", maxCSVRows)})
			break
		}

		m := parsedMatch{
			Line:     line,
			Kickoff:  get(rec, "kickoff"),
			League:   get(rec, "league"),
			HomeTeam: get(rec, "home_team"),
			AwayTeam: get(rec, "away_team"),
			OddsBP:   map[string]int{},
		}

		m.Date = getAny(rec, "date", "match_date")
		if m.Date != "" {
			if !dateRe.MatchString(m.Date) {
				errs = append(errs, rowError{Line: line, Message: "date must be YYYY-MM-DD"})
				continue
			}
			if _, err := time.Parse("2006-01-02", m.Date); err != nil {
				errs = append(errs, rowError{Line: line, Message: "invalid date"})
				continue
			}
		}

		if !kickoffRe.MatchString(m.Kickoff) {
			errs = append(errs, rowError{Line: line, Message: "kickoff must be HH:MM (24h)"})
			continue
		}
		if m.HomeTeam == "" || m.AwayTeam == "" {
			errs = append(errs, rowError{Line: line, Message: "home_team and away_team are required"})
			continue
		}
		if strings.EqualFold(m.HomeTeam, m.AwayTeam) {
			errs = append(errs, rowError{Line: line, Message: "home_team and away_team cannot be the same"})
			continue
		}

		rawMarkets := strings.FieldsFunc(get(rec, "markets"), func(r rune) bool {
			return r == '|' || r == ',' || r == ';'
		})
		marketSet := map[string]bool{}
		badMarket := ""
		for _, rm := range rawMarkets {
			if strings.TrimSpace(rm) == "" {
				continue
			}
			nm, ok := normalizeMarket(rm)
			if !ok {
				badMarket = strings.TrimSpace(rm)
				break
			}
			marketSet[nm] = true
		}
		if badMarket != "" {
			errs = append(errs, rowError{Line: line, Message: "unknown market: " + badMarket})
			continue
		}
		if len(marketSet) == 0 {
			errs = append(errs, rowError{Line: line, Message: "at least one market is required (ft_over_0_5 / ht_over_0_5)"})
			continue
		}
		if marketSet[marketFTOver05] {
			m.Markets = append(m.Markets, marketFTOver05)
		}
		if marketSet[marketHTOver05] {
			m.Markets = append(m.Markets, marketHTOver05)
		}

		badOdds := ""
		for _, mk := range m.Markets {
			cell := ""
			if mk == marketFTOver05 {
				cell = getAny(rec, "ft_odds", "odds_ft", "ft_over_0_5_odds")
			} else {
				cell = getAny(rec, "ht_odds", "odds_ht", "ht_over_0_5_odds")
			}
			bp, present, oddsErr := parseOddsBP(cell)
			if oddsErr != nil {
				badOdds = mk + " odds: " + oddsErr.Error()
				break
			}
			if present {
				m.OddsBP[mk] = bp
			}
		}
		if badOdds != "" {
			errs = append(errs, rowError{Line: line, Message: badOdds})
			continue
		}

		conf := normalizeConfidence(get(rec, "confidence"))
		if conf == "" {
			errs = append(errs, rowError{Line: line, Message: "confidence must be HIGH, MEDIUM or LOW"})
			continue
		}
		m.Confidence = conf

		key := m.Date + "\x00" + strings.ToLower(m.HomeTeam) + "\x00" + strings.ToLower(m.AwayTeam)
		if prev, dup := seen[key]; dup {
			errs = append(errs, rowError{
				Line:    line,
				Message: fmt.Sprintf("duplicate fixture (already on line %d)", prev),
			})
			continue
		}
		seen[key] = line
		good = append(good, m)
	}

	return good, errs, nil
}

// handleUploadMatches accepts multipart form data:
//
//	date     YYYY-MM-DD fallback for rows with no CSV date (optional)
//	replace  "true" to overwrite the affected dates
//	file     the CSV; each row's own date decides where it is stored
func (s *Server) handleUploadMatches(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVBytes+4096)
	if err := r.ParseMultipartForm(maxCSVBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large or malformed"})
		return
	}

	fallbackDate := strings.TrimSpace(r.FormValue("date"))
	if fallbackDate != "" && !dateRe.MatchString(fallbackDate) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	if fallbackDate != "" {
		if _, err := time.Parse("2006-01-02", fallbackDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date"})
			return
		}
	}
	replace := strings.EqualFold(r.FormValue("replace"), "true")

	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "CSV file is required"})
		return
	}
	defer file.Close()

	good, rowErrs, err := parseMatchesCSV(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(good) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "no valid rows",
			"row_errors": rowErrs,
		})
		return
	}

	// A CSV date wins; the form date only fills rows that have none.
	dated := make([]parsedMatch, 0, len(good))
	for _, m := range good {
		if m.Date == "" {
			if fallbackDate == "" {
				rowErrs = append(rowErrs, rowError{Line: m.Line, Message: "date is required in the CSV when no fallback date is set"})
				continue
			}
			m.Date = fallbackDate
		}
		dated = append(dated, m)
	}
	good = dated
	if len(good) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "no rows with a usable date",
			"row_errors": rowErrs,
		})
		return
	}

	dates := []string{}
	dateSeen := map[string]bool{}
	for _, m := range good {
		if !dateSeen[m.Date] {
			dateSeen[m.Date] = true
			dates = append(dates, m.Date)
		}
	}
	sort.Strings(dates)

	ctx := r.Context()

	// Duplicate detection is per date, including dates inside the same file.
	existing := map[string]map[string]bool{}
	for _, d := range dates {
		existing[d] = map[string]bool{}
		rows, err := s.Pool.Query(ctx, `
			SELECT lower(home_team), lower(away_team) FROM matches WHERE match_date = $1
		`, d)
		if err == nil {
			for rows.Next() {
				var h, a string
				if err := rows.Scan(&h, &a); err == nil {
					existing[d][h+"\x00"+a] = true
				}
			}
			rows.Close()
		}
	}

	var conflicts []string
	for _, m := range good {
		key := strings.ToLower(m.HomeTeam) + "\x00" + strings.ToLower(m.AwayTeam)
		if existing[m.Date][key] {
			conflicts = append(conflicts, m.Date+": "+m.HomeTeam+" vs "+m.AwayTeam)
		}
	}
	if len(conflicts) > 0 && !replace {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":      "matches already exist for these dates",
			"duplicates": conflicts,
			"hint":       "re-send with replace=true to overwrite the affected dates",
		})
		return
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	if replace {
		// Never wipe money history. Refuse a date that already has bets or
		// archived results; betting/refund must be resolved first.
		for _, d := range dates {
			var betCount, resultCount int
			_ = tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM bets b JOIN matches m ON m.id = b.match_id
				WHERE m.match_date = $1
			`, d).Scan(&betCount)
			_ = tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM results_archive r JOIN matches m ON m.id = r.match_id
				WHERE m.match_date = $1
			`, d).Scan(&resultCount)
			if betCount > 0 || resultCount > 0 {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": fmt.Sprintf("cannot replace %s: %d bet(s) and %d archived result(s) already exist", d, betCount, resultCount),
				})
				return
			}
			if _, err := tx.Exec(ctx, `DELETE FROM matches WHERE match_date = $1`, d); err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot replace existing matches for " + d})
				return
			}
		}
	}

	imported := 0
	for _, m := range good {
		var matchID string
		err := tx.QueryRow(ctx, `
			INSERT INTO matches (match_date, kickoff_time, league, home_team, away_team, status, confidence)
			VALUES ($1, $2, $3, $4, $5, 'upcoming', $6)
			RETURNING id
		`, m.Date, m.Kickoff+":00", m.League, m.HomeTeam, m.AwayTeam, m.Confidence).Scan(&matchID)
		if err != nil {
			rowErrs = append(rowErrs, rowError{Line: m.Line, Message: "insert failed: " + err.Error()})
			continue
		}
		for _, mk := range m.Markets {
			var odds interface{}
			if bp, ok := m.OddsBP[mk]; ok && bp > 0 {
				odds = bp
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO match_markets (match_id, market, odds_bp) VALUES ($1, $2, $3)
				ON CONFLICT (match_id, market) DO UPDATE SET odds_bp = EXCLUDED.odds_bp
			`, matchID, mk, odds); err != nil {
				rowErrs = append(rowErrs, rowError{Line: m.Line, Message: "market insert failed"})
			}
		}
		imported++
	}

	if imported == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error":      "no rows imported",
			"row_errors": rowErrs,
		})
		return
	}

	fileName := ""
	if hdr != nil {
		fileName = hdr.Filename
	}

	// One import-log row per date in the file (counts[3] = total, ok, failed).
	counts := map[string][3]int{}
	for _, m := range good {
		c := counts[m.Date]
		c[0]++
		c[1]++
		counts[m.Date] = c
	}
	if len(rowErrs) > 0 {
		d := fallbackDate
		if d == "" && len(dates) > 0 {
			d = dates[0]
		}
		if d != "" {
			c := counts[d]
			c[0] += len(rowErrs)
			c[2] += len(rowErrs)
			counts[d] = c
		}
	}
	for _, d := range dates {
		c := counts[d]
		if _, err := tx.Exec(ctx, `
			INSERT INTO import_log
				(admin_name, match_date, file_name, rows_total, rows_imported, rows_failed, replaced)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, adminName(r), d, fileName, c[0], c[1], c[2], replace); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "log write failed"})
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	firstDate := ""
	if len(dates) > 0 {
		firstDate = dates[0]
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"date":       firstDate,
		"dates":      dates,
		"days":       len(dates),
		"imported":   imported,
		"failed":     len(rowErrs),
		"replaced":   replace,
		"row_errors": rowErrs,
	})
}

// handleListMatches is the public Home feed: today and tomorrow only.
func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Public feed, but an inactive signed-in user may only preview three
	// matches. Anonymous visitors see the full published list.
	var previewLimit *int64
	if tok := bearerToken(r); tok != "" {
		if claims, err := s.parseToken(tok); err == nil && !claims.IsAdmin {
			if active, err := s.userActive(ctx, claims.Subject); err == nil && !active {
				n := int64(3)
				previewLimit = &n
			}
		}
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, to_char(m.match_date, 'YYYY-MM-DD'), to_char(m.kickoff_time, 'HH24:MI'), COALESCE(m.league, ''),
		       m.home_team, m.away_team, m.status, m.confidence,
		       COALESCE(array_agg(mm.market ORDER BY mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}'),
		       COALESCE(array_agg(COALESCE(mm.odds_bp, 0) ORDER BY mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}'),
		       (m.status = 'upcoming'
		         AND (m.match_date + m.kickoff_time) > (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')),
		       (m.settled_at IS NOT NULL),
		       m.home_goals, m.away_goals, m.home_goals_ht, m.away_goals_ht
		FROM matches m
		LEFT JOIN match_markets mm ON mm.match_id = m.id
		WHERE m.match_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date
		  AND m.match_date <= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date + 1
		GROUP BY m.id
		ORDER BY m.match_date, m.kickoff_time, m.home_team
		LIMIT $1
	`, previewLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type matchRow struct {
		ID          string   `json:"id"`
		MatchDate   string   `json:"match_date"`
		Kickoff     string   `json:"kickoff"`
		League      string   `json:"league"`
		HomeTeam    string   `json:"home_team"`
		AwayTeam    string   `json:"away_team"`
		Status      string   `json:"status"`
		Confidence  string   `json:"confidence"`
		Markets     []string       `json:"markets"`
		MarketOdds  map[string]int `json:"market_odds"`
		IsOpen      bool           `json:"is_open"`
		Settled     bool           `json:"settled"`
		HomeGoals   *int           `json:"home_goals"`
		AwayGoals   *int           `json:"away_goals"`
		HomeGoalsHT *int           `json:"home_goals_ht"`
		AwayGoalsHT *int           `json:"away_goals_ht"`
	}

	out := []matchRow{}
	for rows.Next() {
		var m matchRow
		var odds []int
		if err := rows.Scan(&m.ID, &m.MatchDate, &m.Kickoff, &m.League, &m.HomeTeam,
			&m.AwayTeam, &m.Status, &m.Confidence, &m.Markets, &odds, &m.IsOpen,
			&m.Settled, &m.HomeGoals, &m.AwayGoals, &m.HomeGoalsHT, &m.AwayGoalsHT); err != nil {
			continue
		}
		m.MarketOdds = map[string]int{}
		for i, mk := range m.Markets {
			if i < len(odds) {
				m.MarketOdds[mk] = odds[i]
			}
		}
		out = append(out, m)
	}

	// The body already encodes the preview/full variant, so a body-derived
	// ETag is safe. Vary plus private caching keep the two auth states apart
	// in browser caches and out of shared caches entirely.
	writeCachedJSON(w, r, http.StatusOK, "private, max-age=60", true, map[string]interface{}{"matches": out, "preview": previewLimit != nil})
}

// handleAdminListMatches lists every match for one date, with its markets.
func (s *Server) handleAdminListMatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dateStr := strings.TrimSpace(r.URL.Query().Get("date"))
	if !dateRe.MatchString(dateStr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, to_char(m.match_date, 'YYYY-MM-DD'), to_char(m.kickoff_time, 'HH24:MI'), COALESCE(m.league, ''),
		       m.home_team, m.away_team, m.status, m.confidence,
		       COALESCE(array_agg(mm.market ORDER BY mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}'),
		       COALESCE(array_agg(COALESCE(mm.odds_bp, 0) ORDER BY mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}'),
		       (m.settled_at IS NOT NULL),
		       COALESCE(m.home_goals, -1), COALESCE(m.away_goals, -1),
		       COALESCE(m.home_goals_ht, -1), COALESCE(m.away_goals_ht, -1)
		FROM matches m
		LEFT JOIN match_markets mm ON mm.match_id = m.id
		WHERE m.match_date = $1
		GROUP BY m.id
		ORDER BY m.kickoff_time, m.home_team
	`, dateStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type matchRow struct {
		ID          string   `json:"id"`
		MatchDate   string   `json:"match_date"`
		Kickoff     string   `json:"kickoff"`
		League      string   `json:"league"`
		HomeTeam    string   `json:"home_team"`
		AwayTeam    string   `json:"away_team"`
		Status      string   `json:"status"`
		Confidence  string   `json:"confidence"`
		Markets     []string       `json:"markets"`
		MarketOdds  map[string]int `json:"market_odds"`
		Settled     bool           `json:"settled"`
		HomeGoals   int            `json:"home_goals"`
		AwayGoals   int            `json:"away_goals"`
		HomeGoalsHT int            `json:"home_goals_ht"`
		AwayGoalsHT int            `json:"away_goals_ht"`
	}

	out := []matchRow{}
	for rows.Next() {
		var m matchRow
		var odds []int
		if err := rows.Scan(&m.ID, &m.MatchDate, &m.Kickoff, &m.League, &m.HomeTeam,
			&m.AwayTeam, &m.Status, &m.Confidence, &m.Markets, &odds,
			&m.Settled, &m.HomeGoals, &m.AwayGoals, &m.HomeGoalsHT, &m.AwayGoalsHT); err != nil {
			continue
		}
		m.MarketOdds = map[string]int{}
		for i, mk := range m.Markets {
			if i < len(odds) {
				m.MarketOdds[mk] = odds[i]
			}
		}
		out = append(out, m)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"date":    dateStr,
		"matches": out,
	})
}

// handleAdminUpdateMatch changes confidence or status for one match.
// Body: {confidence?, status?}
func (s *Server) handleAdminUpdateMatch(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing match id"})
		return
	}

	var body struct {
		Confidence string `json:"confidence"`
		Status     string `json:"status"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	ctx := r.Context()
	if body.Confidence != "" {
		conf := normalizeConfidence(body.Confidence)
		if conf == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid confidence"})
			return
		}
		// Confidence may be changed, but the UI and history keep the value
		// that was live when the bet was placed (stored on the bet row later).
		if _, err := s.Pool.Exec(ctx, `
			UPDATE matches SET confidence = $1, updated_at = NOW() WHERE id = $2
		`, conf, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}

	if body.Status != "" {
		switch body.Status {
		case "upcoming", "live", "awaiting_result", "finished", "postponed", "cancelled":
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
			return
		}

		var settledAt *string
		if err := s.Pool.QueryRow(ctx, `SELECT settled_at::text FROM matches WHERE id = $1`, id).Scan(&settledAt); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "match not found"})
			return
		}
		if settledAt != nil {
			if body.Status == "finished" {
				// Already finished: a stale admin page saving the same status
				// is a harmless no-op.
				writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
				return
			}
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "match is settled — correct the result instead of changing its status",
			})
			return
		}

		// 'finished' is written only by settlement, which records the score,
		// settles every slip and notifies bettors in one transaction. Letting
		// this dropdown set it directly is how a match was shown as completed
		// with no result at all.
		if body.Status == "finished" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "use the Settle action to finish a match — it records the score and settles slips",
			})
			return
		}

		if _, err := s.Pool.Exec(ctx, `
			UPDATE matches SET status = $1, updated_at = NOW() WHERE id = $2
		`, body.Status, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleAdminDeleteMatch removes a wrongly uploaded match. It is allowed only
// before kickoff while the match is neither settled nor finished. Money
// history is never deleted: any bet or archived result blocks the delete.
// Body (optional): {"reason":"..."}
func (s *Server) handleAdminDeleteMatch(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing match id"})
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	reason := strings.TrimSpace(body.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	var matchDate, home, away, kickoff, status string
	var settledAt *time.Time
	var beforeKickoff bool
	err = tx.QueryRow(ctx, `
		SELECT to_char(match_date, 'YYYY-MM-DD'), home_team, away_team, to_char(kickoff_time, 'HH24:MI'),
		       status, settled_at,
		       (match_date + kickoff_time) > (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')
		FROM matches WHERE id = $1 FOR UPDATE
	`, id).Scan(&matchDate, &home, &away, &kickoff, &status, &settledAt, &beforeKickoff)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "match not found"})
		return
	}
	if settledAt != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "match is already settled — use a correction instead"})
		return
	}
	switch status {
	case "finished", "awaiting_result", "live", "cancelled":
		writeJSON(w, http.StatusConflict, map[string]string{"error": "match has started or finished — it can no longer be deleted"})
		return
	}
	if !beforeKickoff {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "kickoff has passed — settle or refund this match instead"})
		return
	}

	var betCount, resultCount int
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE match_id = $1`, id).Scan(&betCount)
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM results_archive WHERE match_id = $1`, id).Scan(&resultCount)
	if betCount > 0 || resultCount > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": fmt.Sprintf("cannot delete: %d bet(s) and %d archived result(s) exist — refund them first", betCount, resultCount),
		})
		return
	}

	if _, err := tx.Exec(ctx, `DELETE FROM matches WHERE id = $1`, id); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "match could not be deleted"})
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO match_delete_log
			(match_id, match_date, kickoff_time, home_team, away_team, reason, reviewer)
		VALUES ($1, $2, $3::time, $4, $5, $6, $7)
	`, id, matchDate, kickoff, home, away, reason, adminName(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit write failed"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "deleted",
		"id":         id,
		"match_date": matchDate,
		"home_team":  home,
		"away_team":  away,
	})
}

// handleImportLog returns recent uploads for the admin Matches centre.
func (s *Server) handleImportLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT id, admin_name, to_char(match_date, 'YYYY-MM-DD'), COALESCE(file_name, ''), rows_total,
		       rows_imported, rows_failed, replaced, to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM import_log
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type logRow struct {
		ID           string `json:"id"`
		AdminName    string `json:"admin_name"`
		MatchDate    string `json:"match_date"`
		FileName     string `json:"file_name"`
		RowsTotal    int    `json:"rows_total"`
		RowsImported int    `json:"rows_imported"`
		RowsFailed   int    `json:"rows_failed"`
		Replaced     bool   `json:"replaced"`
		CreatedAt    string `json:"created_at"`
	}

	out := []logRow{}
	for rows.Next() {
		var l logRow
		if err := rows.Scan(&l.ID, &l.AdminName, &l.MatchDate, &l.FileName, &l.RowsTotal,
			&l.RowsImported, &l.RowsFailed, &l.Replaced, &l.CreatedAt); err != nil {
			continue
		}
		out = append(out, l)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"imports": out})
}

// handleMatchHistory returns published matches from the last six months, grouped
// by day, with the signed-in user's bets on each match (if any). It powers the
// History page so users see the status of every admin-posted match, not only
// their own slips.
func (s *Server) handleMatchHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var userID string
	if tok := bearerToken(r); tok != "" {
		if claims, err := s.parseToken(tok); err == nil && !claims.IsAdmin {
			userID = claims.Subject
		}
	}

	// History is a bounded backward lookup, not a live feed. Cap the row count
	// so this endpoint cannot scan and transfer six months of matches on every
	// request; an optional ?limit= is honoured only up to the hard cap.
	const maxHistoryMatches = 500
	historyLimit := maxHistoryMatches
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < historyLimit {
			historyLimit = n
		}
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, to_char(m.match_date, 'YYYY-MM-DD'), to_char(m.kickoff_time, 'HH24:MI'), COALESCE(m.league, ''),
		       m.home_team, m.away_team, m.status, m.confidence,
		       (m.settled_at IS NOT NULL),
		       m.home_goals, m.away_goals, m.home_goals_ht, m.away_goals_ht,
		       COALESCE(array_agg(mm.market ORDER BY mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}')
		FROM matches m
		LEFT JOIN match_markets mm ON mm.match_id = m.id
		WHERE m.match_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date - INTERVAL '6 months'
		GROUP BY m.id
		ORDER BY m.match_date DESC, m.kickoff_time, m.home_team
		LIMIT $1
	`, historyLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type matchRow struct {
		ID          string   `json:"id"`
		MatchDate   string   `json:"match_date"`
		Kickoff     string   `json:"kickoff"`
		League      string   `json:"league"`
		HomeTeam    string   `json:"home_team"`
		AwayTeam    string   `json:"away_team"`
		Status      string   `json:"status"`
		Confidence  string   `json:"confidence"`
		Settled     bool     `json:"settled"`
		HomeGoals   *int     `json:"home_goals"`
		AwayGoals   *int     `json:"away_goals"`
		HomeGoalsHT *int     `json:"home_goals_ht"`
		AwayGoalsHT *int     `json:"away_goals_ht"`
		Markets     []string `json:"markets"`
		MyBets      []struct {
			Market     string `json:"market"`
			Status     string `json:"status"`
			StakeCents int64  `json:"stake_cents"`
			PayoutCents int64 `json:"payout_cents"`
			OddsBP     int64  `json:"odds_bp"`
		} `json:"my_bets"`
	}

	// Collect matches first.
	matches := []matchRow{}
	matchIDs := []string{}
	for rows.Next() {
		var m matchRow
		m.MyBets = []struct {
			Market      string `json:"market"`
			Status      string `json:"status"`
			StakeCents  int64  `json:"stake_cents"`
			PayoutCents int64  `json:"payout_cents"`
			OddsBP      int64  `json:"odds_bp"`
		}{}
		if err := rows.Scan(&m.ID, &m.MatchDate, &m.Kickoff, &m.League, &m.HomeTeam,
			&m.AwayTeam, &m.Status, &m.Confidence, &m.Settled, &m.HomeGoals,
			&m.AwayGoals, &m.HomeGoalsHT, &m.AwayGoalsHT, &m.Markets); err != nil {
			continue
		}
		matches = append(matches, m)
		matchIDs = append(matchIDs, m.ID)
	}

	// Fetch this user's bets on those matches, if signed in.
	if userID != "" && len(matchIDs) > 0 {
		betRows, err := s.Pool.Query(ctx, `
			SELECT match_id, market, status, stake_cents, payout_cents, COALESCE(payout_multiplier_bp, $2)
			FROM bets
			WHERE user_id = $1 AND match_id = ANY($3)
		`, userID, s.Cfg.PayoutMultiplier, matchIDs)
		if err == nil {
			defer betRows.Close()
			betsByMatch := map[string][]struct {
				Market      string `json:"market"`
				Status      string `json:"status"`
				StakeCents  int64  `json:"stake_cents"`
				PayoutCents int64  `json:"payout_cents"`
				OddsBP      int64  `json:"odds_bp"`
			}{}
			for betRows.Next() {
				var mid, market, status string
				var stake, payout, odds int64
				if err := betRows.Scan(&mid, &market, &status, &stake, &payout, &odds); err != nil {
					continue
				}
				betsByMatch[mid] = append(betsByMatch[mid], struct {
					Market      string `json:"market"`
					Status      string `json:"status"`
					StakeCents  int64  `json:"stake_cents"`
					PayoutCents int64  `json:"payout_cents"`
					OddsBP      int64  `json:"odds_bp"`
				}{Market: market, Status: status, StakeCents: stake, PayoutCents: payout, OddsBP: odds})
			}
			for i := range matches {
				if bs, ok := betsByMatch[matches[i].ID]; ok {
					matches[i].MyBets = bs
				}
			}
		}
	}

	// Group by date.
	dayMap := map[string][]matchRow{}
	dayOrder := []string{}
	for _, m := range matches {
		if _, ok := dayMap[m.MatchDate]; !ok {
			dayOrder = append(dayOrder, m.MatchDate)
		}
		dayMap[m.MatchDate] = append(dayMap[m.MatchDate], m)
	}
	type dayGroup struct {
		Date    string     `json:"date"`
		Matches []matchRow `json:"matches"`
	}
	days := []dayGroup{}
	for _, d := range dayOrder {
		days = append(days, dayGroup{Date: d, Matches: dayMap[d]})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"days": days})
}
