package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type aiSuggestionRequest struct {
	MatchID   string `json:"match_id"`
	SourceURL string `json:"source_url"`
}

type groqResult struct {
	HomeGoals   *int   `json:"home_goals"`
	AwayGoals   *int   `json:"away_goals"`
	HomeGoalsHT *int   `json:"home_goals_ht"`
	AwayGoalsHT *int   `json:"away_goals_ht"`
	Confidence  string `json:"confidence"`
	Notes       string `json:"notes"`
}

// handleAdminGenerateSuggestion fetches a result page, asks Groq to extract the
// final score as strict JSON, validates it, and stores it as a pending
// suggestion. It never settles a bet and never touches the ledger.
func (s *Server) handleAdminGenerateSuggestion(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.GroqAPIKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "GROQ_API_KEY is not configured on this server",
		})
		return
	}

	var body aiSuggestionRequest
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	body.MatchID = strings.TrimSpace(body.MatchID)
	body.SourceURL = strings.TrimSpace(body.SourceURL)
	if body.MatchID == "" || body.SourceURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "match_id and source_url are required"})
		return
	}
	u, err := url.Parse(body.SourceURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_url must be an http(s) URL"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	// 1. Fetch the page. Reject internal/loopback/metadata addresses and any
	// hostname that resolves to them, then pin the dial to vetted IPs.
	client, err := publicHTTPClient(ctx, u)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "source_url host is not public",
		})
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not build request"})
		return
	}
	req.Header.Set("User-Agent", "BetvaResultBot/1.0 (+https://betva)")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ai source fetch failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not fetch source"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("source returned HTTP %d", resp.StatusCode)})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not read source"})
		return
	}
	excerpt := cleanHTML(string(raw))
	if len(excerpt) > 8000 {
		excerpt = excerpt[:8000]
	}
	if strings.TrimSpace(excerpt) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "source page had no readable text"})
		return
	}

	// 2. Ask Groq for strict JSON.
	systemPrompt := `You extract football match results from page text.
Return ONLY a single JSON object. No markdown, no code fences, no prose.
Schema:
{"home_goals":integer,"away_goals":integer,"home_goals_ht":integer or null,"away_goals_ht":integer or null,"confidence":"high" or "medium" or "low","notes":string}
Rules:
- home_goals and away_goals are required non-negative integers (full-time score).
- home_goals_ht / away_goals_ht are the half-time score, or null if not stated.
- confidence reflects how clearly the source states the result.
- notes is one short sentence, max 120 characters.
- Output the JSON object and nothing else.`
	userPrompt := fmt.Sprintf("Page text:\n%s", excerpt)

	groqBody, _ := json.Marshal(map[string]interface{}{
		"model": s.Cfg.GroqModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0,
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "groq_result",
				"strict": true,
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"home_goals":     map[string]interface{}{"type": "integer"},
						"away_goals":     map[string]interface{}{"type": "integer"},
						"home_goals_ht":  map[string]interface{}{"type": []string{"integer", "null"}},
						"away_goals_ht":  map[string]interface{}{"type": []string{"integer", "null"}},
						"confidence":     map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}},
						"notes":          map[string]interface{}{"type": "string"},
					},
					"required":             []string{"home_goals", "away_goals", "home_goals_ht", "away_goals_ht", "confidence", "notes"},
					"additionalProperties": false,
				},
			},
		},
		"max_tokens": 256,
	})
	gReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(groqBody))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not build groq request"})
		return
	}
	gReq.Header.Set("Authorization", "Bearer "+s.Cfg.GroqAPIKey)
	gReq.Header.Set("Content-Type", "application/json")
	gResp, err := (&http.Client{Timeout: 20 * time.Second}).Do(gReq)
	if err != nil {
		log.Printf("groq request failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "groq request failed"})
		return
	}
	defer gResp.Body.Close()
	gRaw, _ := io.ReadAll(io.LimitReader(gResp.Body, 1<<20))
	if gResp.StatusCode < 200 || gResp.StatusCode >= 300 {
		log.Printf("groq returned HTTP %d: %s", gResp.StatusCode, strings.TrimSpace(string(gRaw)))
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("groq returned HTTP %d", gResp.StatusCode),
		})
		return
	}

	var groqEnvelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(gRaw, &groqEnvelope); err != nil || len(groqEnvelope.Choices) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "groq returned an unexpected shape"})
		return
	}
	content := strings.TrimSpace(groqEnvelope.Choices[0].Message.Content)
	content = extractJSON(content)

	// 3. Validate strict JSON.
	var parsed groqResult
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		storeFailedSuggestion(ctx, s, body.MatchID, body.SourceURL, excerpt, content, groqEnvelope.Model, "groq returned invalid JSON")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "groq returned invalid JSON", "raw": content})
		return
	}
	if parsed.HomeGoals == nil || parsed.AwayGoals == nil {
		storeFailedSuggestion(ctx, s, body.MatchID, body.SourceURL, excerpt, content, groqEnvelope.Model, "missing home/away goals")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "groq did not return both goals", "raw": content})
		return
	}
	if *parsed.HomeGoals < 0 || *parsed.HomeGoals > 30 || *parsed.AwayGoals < 0 || *parsed.AwayGoals > 30 {
		storeFailedSuggestion(ctx, s, body.MatchID, body.SourceURL, excerpt, content, groqEnvelope.Model, "goals out of range")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "goals out of range", "raw": content})
		return
	}
	if parsed.HomeGoalsHT != nil && (*parsed.HomeGoalsHT < 0 || *parsed.HomeGoalsHT > 30) {
		storeFailedSuggestion(ctx, s, body.MatchID, body.SourceURL, excerpt, content, groqEnvelope.Model, "ht goals out of range")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "half-time goals out of range", "raw": content})
		return
	}
	if parsed.AwayGoalsHT != nil && (*parsed.AwayGoalsHT < 0 || *parsed.AwayGoalsHT > 30) {
		storeFailedSuggestion(ctx, s, body.MatchID, body.SourceURL, excerpt, content, groqEnvelope.Model, "ht goals out of range")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "half-time goals out of range", "raw": content})
		return
	}
	conf := strings.ToLower(strings.TrimSpace(parsed.Confidence))
	if conf != "high" && conf != "medium" && conf != "low" {
		conf = "low"
	}
	if utf8RuneCount(parsed.Notes) > 120 {
		parsed.Notes = string([]rune(parsed.Notes)[:120])
	}

	// 4. Store as pending suggestion.
	var id string
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO result_suggestions
		  (match_id, source_url, source_excerpt, suggested_home, suggested_away,
		   suggested_home_ht, suggested_away_ht, confidence, provider, model, raw_response, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'groq',$9,$10,'pending')
		RETURNING id
	`, body.MatchID, body.SourceURL, excerpt, *parsed.HomeGoals, *parsed.AwayGoals,
		parsed.HomeGoalsHT, parsed.AwayGoalsHT, conf, groqEnvelope.Model, []byte(content)).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store suggestion"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":                id,
		"match_id":          body.MatchID,
		"suggested_home":    *parsed.HomeGoals,
		"suggested_away":    *parsed.AwayGoals,
		"suggested_home_ht": parsed.HomeGoalsHT,
		"suggested_away_ht": parsed.AwayGoalsHT,
		"confidence":        conf,
		"notes":             parsed.Notes,
		"model":             groqEnvelope.Model,
		"status":            "pending",
	})
}

func storeFailedSuggestion(ctx context.Context, s *Server, matchID, sourceURL, excerpt, raw, model, reason string) {
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO result_suggestions
		  (match_id, source_url, source_excerpt, raw_response, provider, model, status, review_reason)
		VALUES ($1,$2,$3,$4,'groq',$5,'failed',$6)
	`, matchID, sourceURL, excerpt, []byte(raw), model, reason)
}

func (s *Server) handleAdminListSuggestions(w http.ResponseWriter, r *http.Request) {
	matchID := strings.TrimSpace(r.URL.Query().Get("match_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = ""
	}
	switch status {
	case "", "pending", "approved", "rejected", "failed":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}

	ctx := r.Context()
	rows, err := s.Pool.Query(ctx, `
		SELECT rs.id, rs.match_id, m.home_team, m.away_team, to_char(m.match_date, 'YYYY-MM-DD'),
		       rs.source_url, rs.suggested_home, rs.suggested_away, rs.suggested_home_ht, rs.suggested_away_ht,
		       COALESCE(rs.confidence, ''), rs.status, COALESCE(rs.reviewed_by, ''),
		       COALESCE(to_char(rs.reviewed_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'), ''),
		       COALESCE(rs.review_reason, ''), rs.model,
		       to_char(rs.created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       (SELECT COALESCE(array_agg(mm.market ORDER BY mm.market), '{}') FROM match_markets mm WHERE mm.match_id = m.id),
		       (SELECT COALESCE(array_agg(COALESCE(mm.odds_bp, 0) ORDER BY mm.market), '{}') FROM match_markets mm WHERE mm.match_id = m.id)
		FROM result_suggestions rs
		JOIN matches m ON m.id = rs.match_id
		WHERE ($1 = '' OR rs.match_id::text = $1)
		  AND ($2 = '' OR rs.status = $2)
		ORDER BY rs.created_at DESC
		LIMIT 100
	`, matchID, status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type row struct {
		ID           string `json:"id"`
		MatchID      string `json:"match_id"`
		HomeTeam     string `json:"home_team"`
		AwayTeam     string `json:"away_team"`
		MatchDate    string `json:"match_date"`
		SourceURL    string `json:"source_url"`
		Home         *int   `json:"suggested_home"`
		Away         *int   `json:"suggested_away"`
		HomeHT       *int   `json:"suggested_home_ht"`
		AwayHT       *int   `json:"suggested_away_ht"`
		Confidence   string `json:"confidence"`
		Status       string `json:"status"`
		ReviewedBy   string `json:"reviewed_by"`
		ReviewedAt   string `json:"reviewed_at"`
		ReviewReason string `json:"review_reason"`
		Model        string         `json:"model"`
		CreatedAt    string         `json:"created_at"`
		MarketOdds   map[string]int `json:"market_odds"`
	}
	out := []row{}
	for rows.Next() {
		var r row
		var markets []string
		var odds []int
		if err := rows.Scan(&r.ID, &r.MatchID, &r.HomeTeam, &r.AwayTeam, &r.MatchDate,
			&r.SourceURL, &r.Home, &r.Away, &r.HomeHT, &r.AwayHT, &r.Confidence, &r.Status,
			&r.ReviewedBy, &r.ReviewedAt, &r.ReviewReason, &r.Model, &r.CreatedAt,
			&markets, &odds); err == nil {
			r.MarketOdds = map[string]int{}
			for i, mk := range markets {
				if i < len(odds) {
					r.MarketOdds[mk] = odds[i]
				}
			}
			out = append(out, r)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"suggestions": out, "count": len(out)})
}

func (s *Server) handleAdminReviewSuggestion(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if body.Status != "approved" && body.Status != "rejected" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be approved or rejected"})
		return
	}
	if body.Status == "rejected" && body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a rejection requires a reason"})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)

	ctx := r.Context()
	tag, err := s.Pool.Exec(ctx, `
		UPDATE result_suggestions
		SET status = $1, reviewed_by = $2, reviewed_at = NOW(), review_reason = NULLIF($3, '')
		WHERE id = $4 AND status = 'pending'
	`, body.Status, adminName(r), body.Reason, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "suggestion not found or already reviewed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

// extractJSON strips code fences and returns the outermost JSON object, so a
// model that wraps its answer in markdown still parses.
func extractJSON(in string) string {
	s := strings.TrimSpace(in)
	// Strip markdown code fences.
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	// Find the first '{' and the matching last '}'.
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// sees readable text. This keeps the payload small and stops the model from
// being distracted by markup.
func cleanHTML(in string) string {
	lower := strings.ToLower(in)
	// Remove script and style blocks entirely.
	for _, tag := range []string{"script", "style", "noscript"} {
		re := regexp.MustCompile(`(?s)<` + tag + `\b[^>]*>.*?</` + tag + `>`)
		lower = re.ReplaceAllString(lower, " ")
	}
	// Work on the original casing for the remaining tag stripping.
	out := in
	// Remove any remaining tags.
	tagRe := regexp.MustCompile(`<[^>]+>`)
	out = tagRe.ReplaceAllString(out, " ")
	// Decode a handful of common entities.
	out = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ", "&#39;", "'", "&quot;", "\"",
	).Replace(out)
	// Collapse whitespace.
	ws := regexp.MustCompile(`\s+`)
	out = ws.ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
