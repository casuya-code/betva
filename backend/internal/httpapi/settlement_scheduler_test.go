package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/betva/backend/internal/config"
)

// insertEndedMatch creates a match whose kickoff is safely in the past and
// returns its id.
func insertEndedMatch(t *testing.T, s *Server, status string, homeGoals *int) string {
	t.Helper()
	var id string
	err := s.Pool.QueryRow(context.Background(), `
		INSERT INTO matches (match_date, kickoff_time, league, home_team, away_team,
		                     status, confidence, home_goals, away_goals)
		VALUES ((NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date - 1, '12:00',
		        'Test League', 'Home FC', 'Away FC', $1, 'medium', $2, $2)
		RETURNING id
	`, status, homeGoals).Scan(&id)
	if err != nil {
		t.Fatalf("insert ended match: %v", err)
	}
	return id
}

func matchState(t *testing.T, s *Server, id string) (status string, settled bool) {
	t.Helper()
	var settledAt *string
	if err := s.Pool.QueryRow(context.Background(), `
		SELECT status, settled_at::text FROM matches WHERE id = $1
	`, id).Scan(&status, &settledAt); err != nil {
		t.Fatalf("read match %s: %v", id, err)
	}
	return status, settledAt != nil
}

// A finished-without-settlement row is the bug users reported: a completed
// match with no result. The sweep must move it to the honest waiting state.
func TestIntegration_SettlementSweep_WaitsForResult(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	s.Cfg.AutoSettleGraceMinutes = 0

	id := insertEndedMatch(t, s, "upcoming", nil)

	s.runSettlementPass()

	status, settled := matchState(t, s, id)
	if settled {
		t.Fatal("match with no result must not be settled")
	}
	if status != "awaiting_result" {
		t.Fatalf("status = %q, want awaiting_result", status)
	}
}

// With a stored score the sweep settles the match, so FINISHED always has a
// result and settled slips behind it.
func TestIntegration_SettlementSweep_SettlesStoredScore(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	s.Cfg.AutoSettleGraceMinutes = 0

	id := insertEndedMatch(t, s, "awaiting_result", intPtr(2))

	s.runSettlementPass()

	status, settled := matchState(t, s, id)
	if !settled {
		t.Fatal("match with a stored score must be settled")
	}
	if status != "finished" {
		t.Fatalf("status = %q, want finished", status)
	}

	var goals int
	if err := s.Pool.QueryRow(context.Background(), `
		SELECT home_goals FROM matches WHERE id = $1
	`, id).Scan(&goals); err != nil || goals != 2 {
		t.Fatalf("home_goals = %d (err %v), want 2", goals, err)
	}
}

// Only an ADMIN-APPROVED suggestion may be used; a pending one must never
// settle a match.
func TestIntegration_SettlementSweep_UsesOnlyApprovedSuggestion(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	s.Cfg.AutoSettleGraceMinutes = 0

	pendingID := insertEndedMatch(t, s, "upcoming", nil)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO result_suggestions (match_id, source_url, suggested_home, suggested_away, status)
		VALUES ($1, 'https://example.com/result', 9, 9, 'pending')
	`, pendingID); err != nil {
		t.Fatalf("insert pending suggestion: %v", err)
	}
	approvedID := insertEndedMatch(t, s, "upcoming", nil)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO result_suggestions (match_id, source_url, suggested_home, suggested_away, status)
		VALUES ($1, 'https://example.com/result', 3, 1, 'approved')
	`, approvedID); err != nil {
		t.Fatalf("insert approved suggestion: %v", err)
	}

	s.runSettlementPass()

	if status, settled := matchState(t, s, pendingID); settled || status != "awaiting_result" {
		t.Fatalf("pending suggestion must not settle: status=%q settled=%v", status, settled)
	}
	if status, settled := matchState(t, s, approvedID); !settled || status != "finished" {
		t.Fatalf("approved suggestion must settle: status=%q settled=%v", status, settled)
	}

	var goals int
	if err := pool.QueryRow(context.Background(), `
		SELECT home_goals FROM matches WHERE id = $1
	`, approvedID).Scan(&goals); err != nil || goals != 3 {
		t.Fatalf("home_goals = %d (err %v), want 3", goals, err)
	}
}

// A web-discovered score is written as an approved suggestion, then settled by
// the normal sweep. No AI and no direct match write is involved.
func TestIntegration_WebResultApprovedThenSettled(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	s.Cfg.AutoSettleGraceMinutes = 0

	id := insertEndedMatch(t, s, "awaiting_result", nil)
	res := &fetchedResult{
		HomeGoals:   2,
		AwayGoals:   1,
		HomeGoalsHT: intp(1),
		AwayGoalsHT: intp(0),
		Source:      "espn",
		SourceURL:   "https://site.api.espn.com/test",
		Detail:      "Test — ESPN 2-1",
	}
	approved, err := s.storeApprovedWebResult(context.Background(), id, res)
	if err != nil || !approved {
		t.Fatalf("store approved result: approved=%v err=%v", approved, err)
	}
	// Idempotent: repeated discovery must not create a second approval.
	again, err := s.storeApprovedWebResult(context.Background(), id, res)
	if err != nil || again {
		t.Fatalf("second approval: approved=%v err=%v", again, err)
	}

	s.runSettlementPass()

	status, settled := matchState(t, s, id)
	if !settled || status != "finished" {
		t.Fatalf("web result must settle: status=%q settled=%v", status, settled)
	}
	var source string
	if err := pool.QueryRow(context.Background(), `
		SELECT source FROM results_archive WHERE match_id = $1 ORDER BY created_at DESC LIMIT 1
	`, id).Scan(&source); err != nil || source != "auto" {
		t.Fatalf("archive source = %q (err %v), want auto", source, err)
	}
}

// The auto-refund path was broken for the same reason as auto-settle: the
// synthetic request had no chi route param. Lock it in.
func TestIntegration_SettlementSweep_RefundsCancelledMatch(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	s.Cfg.AutoSettleGraceMinutes = 0

	id := insertEndedMatch(t, s, "cancelled", nil)

	var userID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (nickname, phone, official_name, password_hash, status, balance_cents)
		VALUES ('refund-test', $1, 'Refund Test', 'x', 'active', 0) RETURNING id
	`, fmt.Sprintf("+25573%07d", time.Now().UnixNano()%1e7)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var betID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO bets (user_id, match_id, market, stake_cents, status)
		VALUES ($1, $2, $3, 50000, 'pending') RETURNING id
	`, userID, id, marketFTOver05).Scan(&betID); err != nil {
		t.Fatalf("seed bet: %v", err)
	}

	s.runSettlementPass()

	status, settled := matchState(t, s, id)
	if settled {
		t.Fatal("cancelled match should not be settled as a result")
	}
	if status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", status)
	}
	var betStatus, voidReason string
	if err := pool.QueryRow(context.Background(), `
		SELECT status, COALESCE(void_reason, '') FROM bets WHERE id = $1
	`, betID).Scan(&betStatus, &voidReason); err != nil {
		t.Fatalf("read bet: %v", err)
	}
	if betStatus != "refunded" {
		t.Fatalf("bet status = %q, want refunded", betStatus)
	}
	if voidReason == "" {
		t.Fatal("refunded bet should carry a reason")
	}
}

// The admin status dropdown must not be able to mark a match finished: that
// was the second path to "completed but no result".
func TestIntegration_AdminCannotSetFinishedDirectly(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)

	id := insertEndedMatch(t, s, "upcoming", nil)

	r := reqWithChiParam(http.MethodPut, "/api/v1/admin/matches/"+id,
		map[string]string{"status": "finished"}, "id", id, nil)
	w := doReq(s.handleAdminUpdateMatch, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", w.Code, w.Body.String())
	}

	status, settled := matchState(t, s, id)
	if settled || status == "finished" {
		t.Fatalf("match was changed despite rejection: status=%q settled=%v", status, settled)
	}
}

// 'awaiting_result' is a valid, honest admin-set status.
func TestIntegration_AdminCanSetAwaitingResult(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)

	id := insertEndedMatch(t, s, "upcoming", nil)

	r := reqWithChiParam(http.MethodPut, "/api/v1/admin/matches/"+id,
		map[string]string{"status": "awaiting_result"}, "id", id, nil)
	w := doReq(s.handleAdminUpdateMatch, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	status, _ := matchState(t, s, id)
	if status != "awaiting_result" {
		t.Fatalf("status = %q, want awaiting_result", status)
	}
}

// A settled match cannot be pushed back into an unfinished state.
func TestIntegration_SettledMatchStatusIsLocked(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)

	id := insertEndedMatch(t, s, "finished", intPtr(1))
	// Make the row genuinely settled so the lock guard is exercised.
	if _, err := pool.Exec(context.Background(), `
		UPDATE matches SET settled_at = NOW() WHERE id = $1
	`, id); err != nil {
		t.Fatalf("mark settled: %v", err)
	}

	r := reqWithChiParam(http.MethodPut, "/api/v1/admin/matches/"+id,
		map[string]string{"status": "upcoming"}, "id", id, nil)
	w := doReq(s.handleAdminUpdateMatch, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

// Guard the timing inputs: a bad interval must not produce a busy loop, and a
// negative grace must not generate an invalid interval.
func TestAutoSettleConfigClamp(t *testing.T) {
	t.Setenv("AUTO_SETTLE_INTERVAL_MINUTES", "0")
	t.Setenv("AUTO_SETTLE_GRACE_MINUTES", "-5")
	cfg := config.Load()
	if cfg.AutoSettleIntervalMinutes < 1 {
		t.Fatalf("interval = %d, want >= 1", cfg.AutoSettleIntervalMinutes)
	}
	if cfg.AutoSettleGraceMinutes < 0 {
		t.Fatalf("grace = %d, want >= 0", cfg.AutoSettleGraceMinutes)
	}
}
