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
)

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

// StartSettlementScheduler runs a background job that settles finished matches
// automatically. It runs once on startup and then every hour.
func StartSettlementScheduler(s *Server) {
	if os.Getenv("AUTO_SETTLE_ENABLED") == "0" {
		return
	}
	go func() {
		run := func() {
			ctx := context.Background()

			// Mark past matches as finished.
			_, _ = s.Pool.Exec(ctx, `
				UPDATE matches
				SET status = 'finished', updated_at = NOW()
				WHERE status = 'upcoming'
				  AND (match_date + kickoff_time) < (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam') - INTERVAL '2 hours'
			`)

			rows, err := s.Pool.Query(ctx, `
				SELECT id, status, home_goals, away_goals, home_goals_ht, away_goals_ht
				FROM matches
				WHERE settled_at IS NULL
				  AND (match_date + kickoff_time) < (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam') - INTERVAL '2 hours'
			`)
			if err != nil {
				log.Printf("auto-settle query: %v", err)
				return
			}
			defer rows.Close()

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
					continue
				}
				cands = append(cands, c)
			}
			rows.Close()

			for _, c := range cands {
				// Auto-refund postponed/cancelled matches.
				if c.status == "postponed" || c.status == "cancelled" {
					body := map[string]string{"status": c.status, "reason": "automated hourly refund"}
					raw, _ := json.Marshal(body)
					req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/matches/"+c.id+"/refund", bytes.NewReader(raw))
					req.Header.Set("Content-Type", "application/json")
					rr := httptest.NewRecorder()
					s.handleAdminRefundMatch(rr, req)
					log.Printf("auto-refund match %s: HTTP %d %s", c.id, rr.Code, rr.Body.String())
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

				// No score yet: try an approved AI suggestion.
				if home < 0 || away < 0 {
					var sh, sa *int
					var shHT, saHT *int
					err := s.Pool.QueryRow(ctx, `
						SELECT suggested_home, suggested_away, suggested_home_ht, suggested_away_ht
						FROM result_suggestions
						WHERE match_id = $1 AND status = 'approved'
						ORDER BY created_at DESC
						LIMIT 1
					`, c.id).Scan(&sh, &sa, &shHT, &saHT)
					if err == nil && sh != nil && sa != nil {
						home = *sh
						away = *sa
						if shHT != nil {
							homeHT = shHT
						}
						if saHT != nil {
							awayHT = saHT
						}
					}
				}

				if home < 0 || away < 0 {
					continue
				}

				h, a := home, away
				body := settleRequest{
					HomeGoals:   &h,
					AwayGoals:   &a,
					HomeGoalsHT: homeHT,
					AwayGoalsHT: awayHT,
					Source:      "auto",
					Reason:      "automated hourly settlement",
					Correct:     false,
				}
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/matches/"+c.id+"/settle", bytes.NewReader(raw))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				s.handleAdminSettleMatch(rr, req)
				log.Printf("auto-settle match %s: HTTP %d %s", c.id, rr.Code, rr.Body.String())
			}
		}

		run()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
