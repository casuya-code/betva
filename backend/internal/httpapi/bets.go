package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type placeBetRequest struct {
	MatchID    string `json:"match_id"`
	Market     string `json:"market"`
	StakeCents int64  `json:"stake_cents"`
}

func validMarket(m string) bool {
	return m == marketFTOver05 || m == marketHTOver05
}

// handlePlaceBet is the money-critical path. Everything happens in one
// transaction: balance lock, debit, bet row, and ledger entry.
func (s *Server) handlePlaceBet(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req placeBetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	req.MatchID = strings.TrimSpace(req.MatchID)
	if req.MatchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "match_id is required"})
		return
	}
	if !validMarket(req.Market) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid market"})
		return
	}
	if !s.Cfg.IsValidStake(req.StakeCents) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":       "invalid stake",
			"stake_tiers": s.Cfg.StakeTiers,
		})
		return
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	// Lock the wallet row so concurrent taps cannot overspend.
	var status string
	var balance int64
	var accessExpiresAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT status, balance_cents, access_expires_at
		FROM users WHERE id = $1 FOR UPDATE
	`, u.ID).Scan(&status, &balance, &accessExpiresAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	if status != "active" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "account is not active — activate your access to place bets",
		})
		return
	}
	if accessExpiresAt != nil && accessExpiresAt.Before(time.Now()) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "access period has expired — renew to place bets",
		})
		return
	}

	// Self-exclusion is a hard block on betting, not a suggestion.
	var excludedUntil *time.Time
	_ = tx.QueryRow(ctx, `
		SELECT MAX(until) FROM self_exclusions WHERE user_id = $1 AND until > NOW()
	`, u.ID).Scan(&excludedUntil)
	if excludedUntil != nil {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"error": "self-exclusion is active — betting is paused",
			"until": excludedUntil.In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02 15:04"),
		})
		return
	}

	// Optional daily cap.
	if s.Cfg.MaxBetPerUserDay > 0 {
		var todayTotal int64
		_ = tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(stake_cents), 0) FROM bets
			WHERE user_id = $1
			  AND (placed_at AT TIME ZONE 'Africa/Dar_es_Salaam')::date
			      = (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date
		`, u.ID).Scan(&todayTotal)
		if todayTotal+req.StakeCents > s.Cfg.MaxBetPerUserDay {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "daily stake limit reached",
			})
			return
		}
	}

	// The match must be open: upcoming and before kickoff.
	var matchStatus, confidence string
	var open bool
	err = tx.QueryRow(ctx, `
		SELECT status, confidence,
		       (status = 'upcoming'
		         AND (match_date + kickoff_time) > (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam'))
		FROM matches WHERE id = $1
	`, req.MatchID).Scan(&matchStatus, &confidence, &open)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "match not found"})
		return
	}
	if !open {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "betting is closed for this match"})
		return
	}

	var offered bool
	_ = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM match_markets WHERE match_id = $1 AND market = $2)
	`, req.MatchID, req.Market).Scan(&offered)
	if !offered {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "market not offered on this match"})
		return
	}

	var already bool
	_ = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM bets WHERE user_id = $1 AND match_id = $2 AND market = $3)
	`, u.ID, req.MatchID, req.Market).Scan(&already)
	if already {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "you already have a slip on this market",
		})
		return
	}

	if balance < req.StakeCents {
		writeJSON(w, http.StatusPaymentRequired, map[string]interface{}{
			"error":   "insufficient balance",
			"balance": balance,
		})
		return
	}

	newBalance := balance - req.StakeCents
	if _, err := tx.Exec(ctx, `UPDATE users SET balance_cents = $1, updated_at = NOW() WHERE id = $2`,
		newBalance, u.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	var betID string
	err = tx.QueryRow(ctx, `
		INSERT INTO bets (user_id, match_id, market, stake_cents, status,
		                  confidence_at_placement, payout_multiplier_bp)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
		RETURNING id
	`, u.ID, req.MatchID, req.Market, req.StakeCents, confidence, s.Cfg.PayoutMultiplier).Scan(&betID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "bet could not be placed"})
		return
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger (user_id, amount_cents, reason, ref_id)
		VALUES ($1, $2, 'bet_placed', $3)
	`, u.ID, -req.StakeCents, betID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ledger write failed"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"bet": map[string]interface{}{
			"id":            betID,
			"match_id":      req.MatchID,
			"market":        req.Market,
			"stake_cents":   req.StakeCents,
			"status":        "pending",
			"confidence":    confidence,
			"payout_if_win": s.Cfg.PayoutFor(req.StakeCents),
		},
		"balance_cents": newBalance,
	})
}

// handleWallet returns the balance plus recent ledger entries.
func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()

	var balance int64
	if err := s.Pool.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id = $1`, u.ID).Scan(&balance); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT id, amount_cents, reason, ref_id,
		       to_char(created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI')
		FROM ledger
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type entry struct {
		ID          string  `json:"id"`
		AmountCents int64   `json:"amount_cents"`
		Reason      string  `json:"reason"`
		RefID       *string `json:"ref_id"`
		CreatedAt   string  `json:"created_at"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.AmountCents, &e.Reason, &e.RefID, &e.CreatedAt); err != nil {
			continue
		}
		out = append(out, e)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"balance_cents": balance,
		"entries":       out,
		"stake_tiers":   s.Cfg.StakeTiers,
	})
}

// handleHistory returns the user's bets for the last six months.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()

	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, b.market, b.stake_cents, b.status, b.payout_cents,
		       to_char(b.placed_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       m.id, to_char(m.match_date, 'YYYY-MM-DD'),
		       to_char(m.kickoff_time, 'HH24:MI'),
		       COALESCE(m.league, ''), m.home_team, m.away_team,
		       m.home_goals, m.away_goals, m.home_goals_ht, m.away_goals_ht,
		       m.status, COALESCE(b.confidence_at_placement, m.confidence),
		       b.void_reason
		FROM bets b
		JOIN matches m ON m.id = b.match_id
		WHERE b.user_id = $1
		  AND b.placed_at >= NOW() - INTERVAL '6 months'
		ORDER BY b.placed_at DESC
		LIMIT 200
	`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type betRow struct {
		ID          string  `json:"id"`
		Market      string  `json:"market"`
		StakeCents  int64   `json:"stake_cents"`
		Status      string  `json:"status"`
		PayoutCents int64   `json:"payout_cents"`
		PlacedAt    string  `json:"placed_at"`
		MatchID     string  `json:"match_id"`
		MatchDate   string  `json:"match_date"`
		Kickoff     string  `json:"kickoff"`
		League      string  `json:"league"`
		HomeTeam    string  `json:"home_team"`
		AwayTeam    string  `json:"away_team"`
		HomeGoals   *int    `json:"home_goals"`
		AwayGoals   *int    `json:"away_goals"`
		HomeGoalsHT *int    `json:"home_goals_ht"`
		AwayGoalsHT *int    `json:"away_goals_ht"`
		MatchStatus string  `json:"match_status"`
		Confidence  string  `json:"confidence"`
		VoidReason  *string `json:"void_reason"`
	}

	out := []betRow{}
	for rows.Next() {
		var b betRow
		if err := rows.Scan(&b.ID, &b.Market, &b.StakeCents, &b.Status, &b.PayoutCents,
			&b.PlacedAt, &b.MatchID, &b.MatchDate, &b.Kickoff, &b.League,
			&b.HomeTeam, &b.AwayTeam, &b.HomeGoals, &b.AwayGoals,
			&b.HomeGoalsHT, &b.AwayGoalsHT, &b.MatchStatus, &b.Confidence,
			&b.VoidReason); err != nil {
			continue
		}
		out = append(out, b)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"bets": out})
}

// handleConfig exposes the betting rules the client needs to render a slip.
// Only user-facing values live here: the admin's DAU target and running-cost
// figures stay behind the admin analytics endpoint.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stake_tiers_cents":    s.Cfg.StakeTiers,
		"payout_multiplier_bp": s.Cfg.PayoutMultiplier,
		"markets":              []string{marketFTOver05, marketHTOver05},
		"push_enabled":         s.Cfg.PushEnabled(),
		"vapid_public_key":     s.Cfg.VAPIDPublicKey,
		"push_daily_cap":       s.Cfg.PushDailyCap,
		"quiet_hours":          map[string]int{"start": s.Cfg.QuietStartHour, "end": s.Cfg.QuietEndHour},
		"access_fee_cents":     s.Cfg.AccessFeeCents,
		"payment_number":       s.Cfg.PaymentNumber,
		"terms_version":        TermsVersion,
	})
}

// handleAdminCredit performs a manual deposit or withdrawal adjustment.
// Body: {amount_cents, reason, allow_negative}
func (s *Server) handleAdminCredit(w http.ResponseWriter, r *http.Request) {
	userID := chiURLParam(r, "id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	var body struct {
		AmountCents   int64  `json:"amount_cents"`
		Reason        string `json:"reason"`
		AllowNegative bool   `json:"allow_negative"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if body.AmountCents == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount_cents must not be zero"})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason is required"})
		return
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	var balance int64
	if err := tx.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&balance); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	newBalance := balance + body.AmountCents
	if newBalance < 0 && !body.AllowNegative {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "adjustment would make the balance negative",
			"balance": balance,
		})
		return
	}

	if _, err := tx.Exec(ctx, `UPDATE users SET balance_cents = $1, updated_at = NOW() WHERE id = $2`,
		newBalance, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ledger (user_id, amount_cents, reason) VALUES ($1, $2, $3)
	`, userID, body.AmountCents, "admin_adjustment: "+body.Reason); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ledger write failed"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"balance_cents": newBalance,
		"amount_cents":  body.AmountCents,
	})
}
