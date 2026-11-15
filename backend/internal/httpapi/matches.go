package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---- CSV contract -------------------------------------------------------
//
// Columns (header row required, order-independent):
//   kickoff     HH:MM (24h, EAT)
//   league      free text (optional)
//   home_team   required
//   away_team   required
//   markets     ft_over_0_5 and/or ht_over_0_5, separated by | or ,
//   confidence  HIGH | MEDIUM | LOW (optional, default MEDIUM)
//
// The upload date comes from the form field `date` (YYYY-MM-DD), so the same
// file shape can be reused for any day.

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
	Line       int      `json:"line"`
	Kickoff    string   `json:"kickoff"`
	League     string   `json:"league"`
	HomeTeam   string   `json:"home_team"`
	AwayTeam   string   `json:"away_team"`
	Markets    []string `json:"markets"`
	Confidence string   `json:"confidence"`
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

		conf := normalizeConfidence(get(rec, "confidence"))
		if conf == "" {
			errs = append(errs, rowError{Line: line, Message: "confidence must be HIGH, MEDIUM or LOW"})
			continue
		}
		m.Confidence = conf

		key := strings.ToLower(m.HomeTeam) + "\x00" + strings.ToLower(m.AwayTeam)
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
//	date     YYYY-MM-DD (required)
//	replace  "true" to overwrite an existing date
//	file     the CSV
func (s *Server) handleUploadMatches(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVBytes+4096)
	if err := r.ParseMultipartForm(maxCSVBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large or malformed"})
		return
	}

	dateStr := strings.TrimSpace(r.FormValue("date"))
	if !dateRe.MatchString(dateStr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	matchDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid date"})
		return
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

	ctx := r.Context()

	// Duplicate detection against what is already stored for this date.
	existing := map[string]bool{}
	rows, err := s.Pool.Query(ctx, `
		SELECT lower(home_team), lower(away_team) FROM matches WHERE match_date = $1
	`, matchDate)
	if err == nil {
		for rows.Next() {
			var h, a string
			if err := rows.Scan(&h, &a); err == nil {
				existing[h+"\x00"+a] = true
			}
		}
		rows.Close()
	}

	var conflicts []string
	for _, m := range good {
		key := strings.ToLower(m.HomeTeam) + "\x00" + strings.ToLower(m.AwayTeam)
		if existing[key] {
			conflicts = append(conflicts, m.HomeTeam+" vs "+m.AwayTeam)
		}
	}
	if len(conflicts) > 0 && !replace {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":      "matches already exist for this date",
			"duplicates": conflicts,
			"hint":       "re-send with replace=true to overwrite the date",
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
		// Refuse to wipe a date that already has bets: that would destroy
		// money history. Bets must be settled or voided first.
		var betCount int
		_ = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM bets b
			JOIN matches m ON m.id = b.match_id
			WHERE m.match_date = $1
		`, matchDate).Scan(&betCount)
		if betCount > 0 {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error": fmt.Sprintf("cannot replace: %d bet(s) already placed on this date", betCount),
			})
			return
		}
		if _, err := tx.Exec(ctx, `DELETE FROM matches WHERE match_date = $1`, matchDate); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "cannot replace existing matches"})
			return
		}
	}

	imported := 0
	for _, m := range good {
		var matchID string
		err := tx.QueryRow(ctx, `
			INSERT INTO matches (match_date, kickoff_time, league, home_team, away_team, status, confidence)
			VALUES ($1, $2, $3, $4, $5, 'upcoming', $6)
			RETURNING id
		`, matchDate, m.Kickoff+":00", m.League, m.HomeTeam, m.AwayTeam, m.Confidence).Scan(&matchID)
		if err != nil {
			rowErrs = append(rowErrs, rowError{Line: m.Line, Message: "insert failed: " + err.Error()})
			continue
		}
		for _, mk := range m.Markets {
			if _, err := tx.Exec(ctx, `
				INSERT INTO match_markets (match_id, market) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, matchID, mk); err != nil {
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

	if _, err := tx.Exec(ctx, `
		INSERT INTO import_log
			(admin_name, match_date, file_name, rows_total, rows_imported, rows_failed, replaced)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, adminName(r), matchDate, fileName, len(good)+len(rowErrs), imported, len(rowErrs), replace); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "log write failed"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	status := http.StatusCreated
	writeJSON(w, status, map[string]interface{}{
		"date":       dateStr,
		"imported":   imported,
		"failed":     len(rowErrs),
		"replaced":   replace,
		"row_errors": rowErrs,
	})
}

// handleListMatches is the public Home feed: today and tomorrow only.
func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, to_char(m.match_date, 'YYYY-MM-DD'), to_char(m.kickoff_time, 'HH24:MI'), COALESCE(m.league, ''),
		       m.home_team, m.away_team, m.status, m.confidence,
		       COALESCE(array_agg(mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}'),
		       (m.status = 'upcoming'
		         AND (m.match_date + m.kickoff_time) > (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam'))
		FROM matches m
		LEFT JOIN match_markets mm ON mm.match_id = m.id
		WHERE m.match_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date
		  AND m.match_date <= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date + 1
		GROUP BY m.id
		ORDER BY m.match_date, m.kickoff_time, m.home_team
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type matchRow struct {
		ID         string   `json:"id"`
		MatchDate  string   `json:"match_date"`
		Kickoff    string   `json:"kickoff"`
		League     string   `json:"league"`
		HomeTeam   string   `json:"home_team"`
		AwayTeam   string   `json:"away_team"`
		Status     string   `json:"status"`
		Confidence string   `json:"confidence"`
		Markets    []string `json:"markets"`
		IsOpen     bool     `json:"is_open"`
	}

	out := []matchRow{}
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.ID, &m.MatchDate, &m.Kickoff, &m.League, &m.HomeTeam,
			&m.AwayTeam, &m.Status, &m.Confidence, &m.Markets, &m.IsOpen); err != nil {
			continue
		}
		out = append(out, m)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"matches": out})
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
		       COALESCE(array_agg(mm.market) FILTER (WHERE mm.market IS NOT NULL), '{}')
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
		ID         string   `json:"id"`
		MatchDate  string   `json:"match_date"`
		Kickoff    string   `json:"kickoff"`
		League     string   `json:"league"`
		HomeTeam   string   `json:"home_team"`
		AwayTeam   string   `json:"away_team"`
		Status     string   `json:"status"`
		Confidence string   `json:"confidence"`
		Markets    []string `json:"markets"`
	}

	out := []matchRow{}
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.ID, &m.MatchDate, &m.Kickoff, &m.League, &m.HomeTeam,
			&m.AwayTeam, &m.Status, &m.Confidence, &m.Markets); err != nil {
			continue
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
		case "upcoming", "live", "finished", "postponed", "cancelled":
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
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
