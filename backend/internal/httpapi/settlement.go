package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type settleRequest struct {
	HomeGoals   *int   `json:"home_goals"`
	AwayGoals   *int   `json:"away_goals"`
	HomeGoalsHT *int   `json:"home_goals_ht"`
	AwayGoalsHT *int   `json:"away_goals_ht"`
	Source      string `json:"source"`
	Reason      string `json:"reason"`
	Correct     bool   `json:"correct"`
}

func validScore(p *int) bool {
	return p != nil && *p >= 0 && *p <= 50
}

// MarketWon decides a single market from the final score.
func MarketWon(market string, home, away int, homeHT, awayHT *int) bool {
	switch market {
	case marketFTOver05:
		return home+away >= 1
	case marketHTOver05:
		if homeHT == nil || awayHT == nil {
			return false
		}
		return *homeHT+*awayHT >= 1
	}
	return false
}

// handleAdminSettleMatch records a result and settles every pending bet.
//
// Corrections are allowed with correct=true: the old payout is reversed with a
// negative ledger entry, the old result stays in the archive, and a new result
// row is written. Nothing is edited in place.
func (s *Server) handleAdminSettleMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chiURLParam(r, "id")
	if matchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing match id"})
		return
	}

	var body settleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if !validScore(body.HomeGoals) || !validScore(body.AwayGoals) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "full-time score is required (0â€“50)"})
		return
	}
	body.Source = strings.TrimSpace(body.Source)
	if body.Source == "" {
		body.Source = "manual"
	}
	body.Reason = strings.TrimSpace(body.Reason)

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	var settledAt *string
	var homeTeam, awayTeam string
	if err := tx.QueryRow(ctx, `
		SELECT settled_at::text, home_team, away_team FROM matches WHERE id = $1 FOR UPDATE
	`, matchID).Scan(&settledAt, &homeTeam, &awayTeam); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "match not found"})
		return
	}
	if settledAt != nil && !body.Correct {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "match already settled â€” send correct=true with a reason to correct it",
		})
		return
	}
	if body.Correct && body.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a correction requires a reason"})
		return
	}

	// Half-time score is mandatory if anyone bet on the HT market.
	var htBets int
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE match_id = $1 AND market = $2`,
		matchID, marketHTOver05).Scan(&htBets)
	if htBets > 0 && (body.HomeGoalsHT == nil || body.AwayGoalsHT == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "half-time score is required because bets exist on the HT market",
		})
		return
	}

	var prevResultID *string
	_ = tx.QueryRow(ctx, `
		SELECT id FROM results_archive WHERE match_id = $1 ORDER BY created_at DESC LIMIT 1
	`, matchID).Scan(&prevResultID)

	reversed := 0
	if body.Correct {
		rows, err := tx.Query(ctx, `
			SELECT id, user_id, status, payout_cents
			FROM bets WHERE match_id = $1 AND settled_at IS NOT NULL
			FOR UPDATE
		`, matchID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
		type oldBet struct {
			ID, UserID, Status string
			Payout             int64
		}
		var olds []oldBet
		for rows.Next() {
			var b oldBet
			if err := rows.Scan(&b.ID, &b.UserID, &b.Status, &b.Payout); err == nil {
				olds = append(olds, b)
			}
		}
		rows.Close()

		for _, b := range olds {
			if b.Status == "win" && b.Payout > 0 {
				if _, err := tx.Exec(ctx, `
					UPDATE users SET balance_cents = balance_cents - $1, updated_at = NOW() WHERE id = $2
				`, b.Payout, b.UserID); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reversal failed"})
					return
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO ledger (user_id, amount_cents, reason, ref_id)
					VALUES ($1, $2, 'bet_correction_reversal', $3)
				`, b.UserID, -b.Payout, b.ID); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reversal ledger failed"})
					return
				}
				reversed++
			}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE bets SET status = 'pending', payout_cents = 0, settled_at = NULL, result_id = NULL
			WHERE match_id = $1 AND settled_at IS NOT NULL
		`, matchID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reset failed"})
			return
		}
	}

	var resultID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO results_archive
			(match_id, result_date, home_goals, away_goals, home_goals_ht, away_goals_ht,
			 source, reviewer, is_correction, corrects_id, reason)
		VALUES ($1, (SELECT match_date FROM matches WHERE id = $1), $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, matchID, *body.HomeGoals, *body.AwayGoals, body.HomeGoalsHT, body.AwayGoalsHT,
		body.Source, adminName(r), body.Correct, prevResultID, body.Reason).Scan(&resultID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "result archive write failed"})
		return
	}

	if _, err := tx.Exec(ctx, `
		UPDATE matches
		SET home_goals = $1, away_goals = $2, home_goals_ht = $3, away_goals_ht = $4,
		    status = 'finished', settled_at = NOW(), updated_at = NOW()
		WHERE id = $5
	`, *body.HomeGoals, *body.AwayGoals, body.HomeGoalsHT, body.AwayGoalsHT, matchID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "match update failed"})
		return
	}

	rows, err := tx.Query(ctx, `
		SELECT id, user_id, market, stake_cents, COALESCE(payout_multiplier_bp, $2)
		FROM bets WHERE match_id = $1 AND settled_at IS NULL
		FOR UPDATE
	`, matchID, s.Cfg.PayoutMultiplier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	type pendingBet struct {
		ID, UserID, Market string
		Stake, Multiplier  int64
	}
	var pending []pendingBet
	for rows.Next() {
		var b pendingBet
		if err := rows.Scan(&b.ID, &b.UserID, &b.Market, &b.Stake, &b.Multiplier); err == nil {
			pending = append(pending, b)
		}
	}
	rows.Close()

	wins, losses := 0, 0
	var paidOut int64
	for _, b := range pending {
		won := MarketWon(b.Market, *body.HomeGoals, *body.AwayGoals, body.HomeGoalsHT, body.AwayGoalsHT)
		status := "loss"
		payout := int64(0)
		if won {
			status = "win"
			payout = b.Stake * b.Multiplier / 10000
			wins++
			paidOut += payout
		} else {
			losses++
		}

		if _, err := tx.Exec(ctx, `
			UPDATE bets SET status = $1, payout_cents = $2, settled_at = NOW(), result_id = $3
			WHERE id = $4
		`, status, payout, resultID, b.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "bet settle failed"})
			return
		}

		if won {
			if _, err := tx.Exec(ctx, `
				UPDATE users SET balance_cents = balance_cents + $1, updated_at = NOW() WHERE id = $2
			`, payout, b.UserID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "payout failed"})
				return
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO ledger (user_id, amount_cents, reason, ref_id)
				VALUES ($1, $2, 'bet_won', $3)
			`, b.UserID, payout, b.ID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "payout ledger failed"})
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	s.notifyMatchBettors(matchID, homeTeam, awayTeam, *body.HomeGoals, *body.AwayGoals, body.Correct)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"match_id":       matchID,
		"result_id":      resultID,
		"bets_settled":   wins + losses,
		"wins":           wins,
		"losses":         losses,
		"paid_out_cents": paidOut,
		"reversed":       reversed,
		"correction":     body.Correct,
	})
}

// notifyMatchBettors writes one results notification per affected user after a
// settlement or correction. Push delivery is throttled separately.
func (s *Server) notifyMatchBettors(matchID, home, away string, hg, ag int, corrected bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := s.Pool.Query(ctx, `
		SELECT user_id, bool_or(status = 'win'), COALESCE(SUM(payout_cents), 0)
		FROM bets
		WHERE match_id = $1 AND status IN ('win', 'loss')
		GROUP BY user_id
	`, matchID)
	if err != nil {
		return
	}
	var winUsers, lossUsers []string
	var winPayout = map[string]int64{}
	for rows.Next() {
		var uid string
		var won bool
		var payout int64
		if err := rows.Scan(&uid, &won, &payout); err == nil {
			if won {
				winUsers = append(winUsers, uid)
				winPayout[uid] = payout
			} else {
				lossUsers = append(lossUsers, uid)
			}
		}
	}
	rows.Close()

	prefix := "Result"
	if corrected {
		prefix = "Corrected result"
	}
	score := fmt.Sprintf("%dâ€“%d", hg, ag)
	for _, uid := range winUsers {
		s.notifyAndPush([]string{uid}, catResults, prefix+": WIN",
			fmt.Sprintf("%s vs %s â€” FT %s. Payout +%s TZS.", home, away, score, formatTZS(winPayout[uid])), "/history")
	}
	for _, uid := range lossUsers {
		s.notifyAndPush([]string{uid}, catResults, prefix+": LOSS",
			fmt.Sprintf("%s vs %s â€” FT %s. Open History to see the settled slip.", home, away, score), "/history")
	}
}

func formatTZS(cents int64) string {
	return strconv.FormatInt(cents/100, 10)
}

// handleAdminRefundMatch refunds every pending bet when a match is postponed
// or cancelled. Postponed bets are never left pending.
func (s *Server) handleAdminRefundMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chiURLParam(r, "id")
	if matchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing match id"})
		return
	}

	var body struct {
		Reason string `json:"reason"`
		Status string `json:"status"` // postponed | cancelled
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if body.Status != "postponed" && body.Status != "cancelled" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be postponed or cancelled"})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		body.Reason = "match " + body.Status
	}

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	var settledAt *string
	var homeTeam, awayTeam string
	if err := tx.QueryRow(ctx, `
		SELECT settled_at::text, home_team, away_team FROM matches WHERE id = $1 FOR UPDATE
	`, matchID).Scan(&settledAt, &homeTeam, &awayTeam); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "match not found"})
		return
	}
	if settledAt != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "match is already settled â€” correct the result instead of refunding",
		})
		return
	}

	rows, err := tx.Query(ctx, `
		SELECT id, user_id, stake_cents FROM bets
		WHERE match_id = $1 AND settled_at IS NULL
		FOR UPDATE
	`, matchID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	type pendingBet struct {
		ID, UserID string
		Stake      int64
	}
	var pending []pendingBet
	for rows.Next() {
		var b pendingBet
		if err := rows.Scan(&b.ID, &b.UserID, &b.Stake); err == nil {
			pending = append(pending, b)
		}
	}
	rows.Close()

	var refunded int64
	for _, b := range pending {
		if _, err := tx.Exec(ctx, `
			UPDATE bets SET status = 'refunded', payout_cents = 0, settled_at = NOW(), void_reason = $1
			WHERE id = $2
		`, body.Reason, b.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refund update failed"})
			return
		}
		if _, err := tx.Exec(ctx, `
			UPDATE users SET balance_cents = balance_cents + $1, updated_at = NOW() WHERE id = $2
		`, b.Stake, b.UserID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refund failed"})
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger (user_id, amount_cents, reason, ref_id)
			VALUES ($1, $2, 'bet_refund', $3)
		`, b.UserID, b.Stake, b.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "refund ledger failed"})
			return
		}
		refunded += b.Stake
	}

	if _, err := tx.Exec(ctx, `
		UPDATE matches SET status = $1, updated_at = NOW() WHERE id = $2
	`, body.Status, matchID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "match update failed"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	refundUsers := make([]string, 0, len(pending))
	for _, b := range pending {
		refundUsers = append(refundUsers, b.UserID)
	}
	if len(refundUsers) > 0 {
		title := "Match postponed â€” stake refunded"
		if body.Status == "cancelled" {
			title = "Match cancelled â€” stake refunded"
		}
		s.notifyAndPush(refundUsers, catResults, title,
			fmt.Sprintf("%s vs %s. %s. %s", homeTeam, awayTeam, body.Reason,
				"Your stake is back in your wallet."), "/profile")
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"match_id":       matchID,
		"bets_refunded":  len(pending),
		"refunded_cents": refunded,
		"status":         body.Status,
	})
}

// handleAdminMatchBets lists bets for one match so the admin can see exposure
// before settling.
func (s *Server) handleAdminMatchBets(w http.ResponseWriter, r *http.Request) {
	matchID := chiURLParam(r, "id")
	if matchID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing match id"})
		return
	}
	ctx := r.Context()

	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, u.nickname, u.phone, b.market, b.stake_cents, b.status,
		       b.payout_cents, COALESCE(b.payout_multiplier_bp, $2),
		       to_char(b.placed_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI')
		FROM bets b
		JOIN users u ON u.id = b.user_id
		WHERE b.match_id = $1
		ORDER BY b.placed_at
	`, matchID, s.Cfg.PayoutMultiplier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type row struct {
		ID          string `json:"id"`
		Nickname    string `json:"nickname"`
		Phone       string `json:"phone"`
		Market      string `json:"market"`
		StakeCents  int64  `json:"stake_cents"`
		Status      string `json:"status"`
		PayoutCents int64  `json:"payout_cents"`
		OddsBP      int64  `json:"odds_bp"`
		PlacedAt    string `json:"placed_at"`
	}
	out := []row{}
	var totalStake int64
	for rows.Next() {
		var b row
		if err := rows.Scan(&b.ID, &b.Nickname, &b.Phone, &b.Market, &b.StakeCents,
			&b.Status, &b.PayoutCents, &b.OddsBP, &b.PlacedAt); err != nil {
			continue
		}
		totalStake += b.StakeCents
		out = append(out, b)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bets":              out,
		"count":             len(out),
		"total_stake_cents": totalStake,
	})
}
