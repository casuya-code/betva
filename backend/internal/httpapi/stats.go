package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"time"
)

// platformRecord is the aggregate tip record behind the trust counter.
//
// It is aggregate, not per-user: everyone sees the same numbers. The unit is a
// published tip (one market on one match), so a settled match with two markets
// counts twice. Only settled matches are counted, and a correction replaces the
// match's current result, so the record never double-counts a corrected match.
type platformRecord struct {
	Total         int     `json:"total"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	WinRatePct    float64 `json:"win_rate_pct"`
	CurrentStreak int     `json:"current_streak"`
	StreakType    string  `json:"streak_type"` // "win", "loss", or "" when nothing is settled
	BestWinStreak int     `json:"best_win_streak"`
}

// handleStats serves the public track record. Losses are always included: the
// counter is only worth anything if it can show a losing run.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Aggregate, not per-user, so one cached blob serves everyone. The counter
	// only changes at settlement; a 60-second cache saves the full scan below
	// on every browser/CDN cache miss without hiding real results.
	if cached, ok := s.statsCache.Get(ctx); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Write(cached)
		return
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT m.settled_at, mm.market,
		       COALESCE(m.home_goals, 0), COALESCE(m.away_goals, 0),
		       m.home_goals_ht, m.away_goals_ht
		FROM matches m
		JOIN match_markets mm ON mm.match_id = m.id
		WHERE m.settled_at IS NOT NULL
		ORDER BY m.settled_at ASC, m.id, mm.market
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	rec := platformRecord{}
	run, runType := 0, ""
	for rows.Next() {
		var (
			settledAt      time.Time
			market         string
			home, away     int
			homeHT, awayHT *int
		)
		if err := rows.Scan(&settledAt, &market, &home, &away, &homeHT, &awayHT); err != nil {
			continue
		}

		won := MarketWon(market, home, away, homeHT, awayHT)
		rec.Total++
		typ := "loss"
		if won {
			rec.Wins++
			typ = "win"
		} else {
			rec.Losses++
		}

		if typ == runType {
			run++
		} else {
			runType, run = typ, 1
		}
		if won && run > rec.BestWinStreak {
			rec.BestWinStreak = run
		}
	}

	rec.CurrentStreak, rec.StreakType = run, runType
	if rec.Total > 0 {
		// One decimal place, e.g. 62.5.
		rec.WinRatePct = math.Round(float64(rec.Wins)*1000/float64(rec.Total)) / 10
	}

	// The counter changes only at settlement, so a short shared cache is safe
	// and saves data on slow connections.
	body, err := json.Marshal(rec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode error"})
		return
	}
	s.statsCache.Set(ctx, body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Write(body)
}
