package httpapi

import (
	"context"
	"math"
	"net/http"
	"time"
)

var eatZone = time.FixedZone("EAT", 3*60*60)

// trackActivity records a user as active for the current EAT day. An in-memory
// seen set makes it a Postgres write only on the user's first authenticated
// request of the day; the ON CONFLICT upsert keeps the record correct across
// restarts and multiple replicas. Rows are append-only: this is the business
// record behind the access-fee decision.
func (s *Server) trackActivity(userID string) {
	if s.Pool == nil || userID == "" || userID == "admin" {
		return
	}
	key := time.Now().In(eatZone).Format("2006-01-02") + ":" + userID

	s.actMu.Lock()
	if s.actSeen == nil {
		s.actSeen = make(map[string]bool)
	}
	// The map only ever holds keys for today; flush older days lazily.
	for k := range s.actSeen {
		if k[:10] != key[:10] {
			delete(s.actSeen, k)
		}
	}
	if s.actSeen[key] {
		s.actMu.Unlock()
		return
	}
	s.actSeen[key] = true
	s.actMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO activity_days (user_id, day)
		VALUES ($1, (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date)
		ON CONFLICT (user_id, day) DO NOTHING
	`, userID)
}

// handleAdminAnalytics serves the daily-active-user record and the access-fee
// planner used for the monthly contribution decision.
func (s *Server) handleAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	days := atoiDefault(r.URL.Query().Get("days"), 30)
	if days < 7 || days > 90 {
		days = 30
	}
	ctx := r.Context()

	type dayRow struct {
		Day      string `json:"day"`
		DAU      int    `json:"dau"`
		NewUsers int    `json:"new_users"`
	}
	byDay := make(map[string]*dayRow, days)

	rows, err := s.Pool.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), COUNT(DISTINCT user_id)
		FROM activity_days
		WHERE day >= ((NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date - $1::int)
		GROUP BY day
	`, days-1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			continue
		}
		byDay[d] = &dayRow{Day: d, DAU: n}
	}
	rows.Close()

	regRows, err := s.Pool.Query(ctx, `
		SELECT to_char((created_at AT TIME ZONE 'Africa/Dar_es_Salaam')::date, 'YYYY-MM-DD'), COUNT(*)
		FROM users
		WHERE created_at >= ((NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date - $1::int)
		GROUP BY 1
	`, days-1)
	if err == nil {
		for regRows.Next() {
			var d string
			var n int
			if err := regRows.Scan(&d, &n); err != nil {
				continue
			}
			if byDay[d] == nil {
				byDay[d] = &dayRow{Day: d}
			}
			byDay[d].NewUsers = n
		}
		regRows.Close()
	}

	// Fill the full window so the chart never shows missing days.
	daysOut := make([]dayRow, 0, days)
	now := time.Now().In(eatZone)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if row, ok := byDay[d]; ok {
			daysOut = append(daysOut, *row)
		} else {
			daysOut = append(daysOut, dayRow{Day: d})
		}
	}

	var dauToday, dau7Sum, peak, dau7N int
	for i, row := range daysOut {
		if i == len(daysOut)-1 {
			dauToday = row.DAU
		}
		if i >= len(daysOut)-7 {
			dau7Sum += row.DAU
			dau7N++
		}
		if row.DAU > peak {
			peak = row.DAU
		}
	}
	avg7 := 0.0
	if dau7N > 0 {
		avg7 = math.Round(float64(dau7Sum)/float64(dau7N)*10) / 10
	}

	var totalUsers, newToday, everActive int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	_ = s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE (created_at AT TIME ZONE 'Africa/Dar_es_Salaam')::date
		    = (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date
	`).Scan(&newToday)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM activity_days`).Scan(&everActive)

	// Access-fee planner: the admin sets the target and the monthly running
	// cost via environment variables; this endpoint only computes the math.
	// Admin-only: none of these figures are exposed in the public config.
	target := s.Cfg.FeeTargetDAU
	cost := s.Cfg.MonthlyCostCents
	suggested := int64(0)
	if target > 0 && cost > 0 {
		users := target
		if dauToday > users {
			users = dauToday
		}
		// Round up to whole 500 TZS steps (50000 cents).
		suggested = ((cost + int64(users)*50000 - 1) / (int64(users) * 50000)) * 50000
	}
	feePlan := map[string]interface{}{
		"target_dau":          target,
		"monthly_cost_cents":  cost,
		"suggested_fee_cents": suggested,
		"reached":             target > 0 && dauToday >= target,
		"configured":          target > 0 && cost > 0,
		// What users currently pay, for comparison when deciding the new rate.
		"current_fee_cents": s.Cfg.AccessFeeCents,
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dau_today":         dauToday,
		"dau_avg_7d":        avg7,
		"dau_peak":          peak,
		"users_total":       totalUsers,
		"users_new_today":   newToday,
		"users_ever_active": everActive,
		"days":              daysOut,
		"fee_plan":          feePlan,
	})
}
