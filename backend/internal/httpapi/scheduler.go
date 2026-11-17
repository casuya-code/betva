package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
)

// settlementLockID is a fixed advisory-lock key so only one API replica runs a
// settlement pass at a time. The settle transaction itself also row-locks the
// match, but the advisory lock avoids every replica doing the same scan and
// logging the same 409s on multi-instance deploys.
const settlementLockID int64 = 0x424554564153544c // "BETVASTL"

// StartPushScheduler retries deferred/failed pushes. Inbox rows are always
// authoritative; this job only makes sure a temporary failure does not cost
// the user the notification forever.
func StartPushScheduler(s *Server) {
	if os.Getenv("PUSH_RETRY_ENABLED") == "0" {
		return
	}
	go func() {
		run := func() {
			if !s.Cfg.PushEnabled() {
				return
			}
			s.dispatchPendingPush(nil)
		}
		run()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// StartSettlementScheduler keeps match results in sync with ended matches.
//
// It runs once on startup and then on a short interval (default 15 minutes):
//   - ended matches with no result move to 'awaiting_result', never 'finished';
//   - an approved AI suggestion is used only as a fallback when no score has
//     been stored on the match by an admin;
//   - refunds are retried while a postponed/cancelled match still has pending
//     slips.
//
// Only settlement writes status='finished', so a finished match always has a
// score and settled bets. AUTO_SETTLE_ENABLED=0 disables the whole job.
func StartSettlementScheduler(s *Server) {
	if !s.Cfg.AutoSettleEnabled {
		return
	}
	interval := time.Duration(s.Cfg.AutoSettleIntervalMinutes) * time.Minute
	go func() {
		s.runSettlementPass()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.runSettlementPass()
		}
	}()
}

// runSettlementPass performs one idempotent settlement sweep.
func (s *Server) runSettlementPass() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Hold a dedicated connection for the advisory lock: pg_advisory_lock is
	// connection-scoped, and the lock must live for the entire pass.
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		log.Printf("auto-settle: acquire connection: %v", err)
		return
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, settlementLockID).Scan(&locked); err != nil {
		log.Printf("auto-settle: advisory lock: %v", err)
		return
	}
	if !locked {
		// Another replica owns this pass; skipping is safe and expected.
		return
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, settlementLockID); err != nil {
			log.Printf("auto-settle: advisory unlock: %v", err)
		}
	}()

	grace := s.Cfg.AutoSettleGraceMinutes

	// Ended matches with no result wait honestly. Never mark them 'finished'
	// here: that is what left users staring at a completed match with no score.
	tag, err := conn.Exec(ctx, `
		UPDATE matches
		SET status = 'awaiting_result', updated_at = NOW()
		WHERE settled_at IS NULL
		  AND status IN ('upcoming', 'live', 'awaiting_result', 'finished')
		  AND (match_date + kickoff_time)
		      < (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam') - make_interval(mins => $1)
	`, grace)
	if err != nil {
		log.Printf("auto-settle: mark awaiting_result: %v", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Printf("auto-settle: marked %d ended match(es) awaiting_result", n)
	}

	// Every unsettled match past the grace window. Postponed/cancelled matches
	// are included only while they still have pending slips, so a completed
	// refund is not retried and re-logged every pass.
	rows, err := conn.Query(ctx, `
		SELECT id, status, home_goals, away_goals, home_goals_ht, away_goals_ht
		FROM matches
		WHERE settled_at IS NULL
		  AND (match_date + kickoff_time)
		      < (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam') - make_interval(mins => $1)
		  AND (
		        status NOT IN ('postponed', 'cancelled')
		        OR EXISTS (
		            SELECT 1 FROM bets b
		            WHERE b.match_id = matches.id AND b.settled_at IS NULL
		        )
		  )
		ORDER BY match_date, kickoff_time
	`, grace)
	if err != nil {
		log.Printf("auto-settle: candidate query: %v", err)
		return
	}

	type candidate struct {
		id     string
		status string
		home   *int
		away   *int
		homeHT *int
		awayHT *int
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.status, &c.home, &c.away, &c.homeHT, &c.awayHT); err != nil {
			log.Printf("auto-settle: scan candidate: %v", err)
			continue
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		log.Printf("auto-settle: iterate candidates: %v", err)
	}
	rows.Close()

	settled, refunded, noResult := 0, 0, 0
	for _, c := range cands {
		ctxMatch, cancelMatch := context.WithTimeout(context.Background(), 30*time.Second)

		// Postponed/cancelled: void any slips still pending.
		if c.status == "postponed" || c.status == "cancelled" {
			rr := callMatchHandler(ctxMatch, s.handleAdminRefundMatch, c.id, map[string]string{
				"status": c.status,
				"reason": "automated settlement sweep",
			})
			cancelMatch()
			log.Printf("auto-refund match %s: HTTP %d %s", c.id, rr.Code, rr.Body.String())
			if rr.Code == http.StatusOK {
				refunded++
			}
			continue
		}

		home, away := -1, -1
		homeHT, awayHT := c.homeHT, c.awayHT
		if c.home != nil {
			home = *c.home
		}
		if c.away != nil {
			away = *c.away
		}

		// Prefer a score an admin stored on the match. Fall back to the newest
		// APPROVED AI suggestion — approval is a human decision, never raw AI.
		if home < 0 || away < 0 {
			var sh, sa *int
			var shHT, saHT *int
			err := s.Pool.QueryRow(ctxMatch, `
				SELECT suggested_home, suggested_away, suggested_home_ht, suggested_away_ht
				FROM result_suggestions
				WHERE match_id = $1 AND status = 'approved'
				ORDER BY created_at DESC
				LIMIT 1
			`, c.id).Scan(&sh, &sa, &shHT, &saHT)
			if err != nil {
				log.Printf("auto-settle: approved suggestion query for %s: %v", c.id, err)
			} else if sh != nil && sa != nil {
				home, away = *sh, *sa
				if shHT != nil {
					homeHT = shHT
				}
				if saHT != nil {
					awayHT = saHT
				}
			}
		}

		if home < 0 || away < 0 {
			// No result source yet. The match stays 'awaiting_result' and will
			// be retried next run; an admin can always settle it manually.
			noResult++
			cancelMatch()
			continue
		}

		h, a := home, away
		rr := callMatchHandler(ctxMatch, s.handleAdminSettleMatch, c.id, settleRequest{
			HomeGoals:   &h,
			AwayGoals:   &a,
			HomeGoalsHT: homeHT,
			AwayGoalsHT: awayHT,
			Source:      "auto",
			Reason:      "automated settlement sweep",
			Correct:     false,
		})
		cancelMatch()
		log.Printf("auto-settle match %s: HTTP %d %s", c.id, rr.Code, rr.Body.String())
		if rr.Code == http.StatusOK {
			settled++
		}
	}

	if len(cands) > 0 {
		log.Printf("auto-settle: pass complete — settled=%d refunded=%d awaiting_result=%d", settled, refunded, noResult)
	}
}

// callMatchHandler invokes an admin handler directly with a synthetic request.
// The scheduler is trusted internal code: admin HTTP middleware (and therefore
// the admin token) has already been bypassed by design here.
func callMatchHandler(ctx context.Context, h http.HandlerFunc, matchID string, body interface{}) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/matches/"+matchID+"/settle", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")

	// chi reads route params from the request context, not the URL string.
	// Without this, every internal settle/refund call failed with
	// "missing match id" and matches were marked finished but never settled.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", matchID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}
