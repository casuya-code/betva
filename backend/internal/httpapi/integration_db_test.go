package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/betva/backend/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openIntegrationPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	url := os.Getenv("BETVA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BETVA_TEST_DATABASE_URL not set; skipping DB integration tests")
	}

	ctx := context.Background()
	schema := "test_" + strings.ReplaceAll(fmt.Sprintf("%d", time.Now().UnixNano()), "-", "_")

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("parse config: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("connect schema pool: %v", err)
	}

	// Ensure search_path on this pool's connections and run migrations.
	if _, err := pool.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()+", public"); err != nil {
		pool.Close()
		admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("set search_path: %v", err)
	}

	root := migrationsRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "*.sql"))
	if err != nil || len(files) == 0 {
		pool.Close()
		admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatalf("find migrations in %s: %v", root, err)
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			pool.Close()
			admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			admin.Close()
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			pool.Close()
			admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			admin.Close()
			t.Fatalf("apply migration %s: %v", filepath.Base(f), err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	}
	return pool, cleanup
}

func migrationsRoot(t *testing.T) string {
	t.Helper()
	// tests run from package dir: backend/internal/httpapi
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(dir, "..", "..", "migrations"))
}

func dbTestServer(pool *pgxpool.Pool) *Server {
	return &Server{
		Pool: pool,
		Cfg: &config.Config{
			Env:              "test",
			JWTSecret:        "db-test-secret-0123456789",
			StakeTiers:       []int64{50000, 100000, 200000, 500000},
			PayoutMultiplier: 15000,
			PushDailyCap:     2,
			QuietStartHour:   22,
			QuietEndHour:     7,
		},
	}
}

func reqWithChiParam(method, target string, body interface{}, key, val string, headers map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, target, &buf)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func doReq(handler http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func jsonMap(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v body=%s", err, w.Body.String())
	}
	return out
}

func TestIntegration_Analytics(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	ctx := context.Background()

	// Two users active today, one yesterday.
	ids := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		var id string
		phone := "+25573" + fmt.Sprintf("%07d", time.Now().UnixNano()%1e7)
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (nickname, phone, official_name, password_hash, status, balance_cents)
			VALUES ('dau', $1, 'DAU Tester', 'x', 'active', 0) RETURNING id
		`, phone).Scan(&id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		ids = append(ids, id)
		s.trackActivity(id)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO activity_days (user_id, day)
		VALUES ($1, ((NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date - 1))
		ON CONFLICT DO NOTHING
	`, ids[0]); err != nil {
		t.Fatalf("seed yesterday: %v", err)
	}

	w := doReq(s.handleAdminAnalytics, httptest.NewRequest("GET", "/api/v1/admin/analytics?days=7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("analytics code=%d body=%s", w.Code, w.Body.String())
	}
	out := jsonMap(t, w)
	if dau, _ := out["dau_today"].(float64); dau < 2 {
		t.Fatalf("dau_today=%v want >=2", out["dau_today"])
	}
	if total, _ := out["users_total"].(float64); total < 2 {
		t.Fatalf("users_total=%v want >=2", out["users_total"])
	}
	days, _ := out["days"].([]interface{})
	if len(days) != 7 {
		t.Fatalf("days=%d want 7", len(days))
	}
	if fp, _ := out["fee_plan"].(map[string]interface{}); fp == nil {
		t.Fatal("missing fee_plan")
	}
}

func TestIntegration_RegisterLogin(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)

	phone := "+25571" + fmt.Sprintf("%07d", time.Now().UnixNano()%1e7)
	regBody := map[string]interface{}{
		"nickname":       "itest",
		"phone":          phone,
		"official_name":  "Integration Tester",
		"password":       "password123",
		"accepted_terms": true,
	}
	w := doJSON(s.handleRegister, "POST", "/api/v1/auth/register", regBody, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("register code=%d body=%s", w.Code, w.Body.String())
	}
	got := jsonMap(t, w)
	if tok, _ := got["token"].(string); tok == "" {
		t.Fatalf("missing token: %v", got)
	}
	user, _ := got["user"].(map[string]interface{})
	if user["phone"] != phone {
		t.Fatalf("phone=%v want %s", user["phone"], phone)
	}

	// login
	w = doJSON(s.handleLogin, "POST", "/api/v1/auth/login", map[string]string{
		"phone": phone, "password": "password123",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", w.Code, w.Body.String())
	}
	got = jsonMap(t, w)
	if tok, _ := got["token"].(string); tok == "" {
		t.Fatalf("missing login token: %v", got)
	}

	// wrong password
	w = doJSON(s.handleLogin, "POST", "/api/v1/auth/login", map[string]string{
		"phone": phone, "password": "wrongpass",
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestIntegration_PlaceSettleAndCorrect(t *testing.T) {
	pool, cleanup := openIntegrationPool(t)
	defer cleanup()
	s := dbTestServer(pool)
	ctx := context.Background()

	// seed active user with balance and access
	phone := "+25572" + fmt.Sprintf("%07d", time.Now().UnixNano()%1e7)
	var userID string
	err := pool.QueryRow(ctx, `
		INSERT INTO users (nickname, phone, official_name, password_hash, status, balance_cents, access_expires_at)
		VALUES ('settler', $1, 'Settle Tester', 'x', 'active', 100000, NOW() + INTERVAL '30 days')
		RETURNING id
	`, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// seed open upcoming match with ft_over_0_5 market
	var matchID string
	err = pool.QueryRow(ctx, `
		INSERT INTO matches (match_date, kickoff_time, league, home_team, away_team, status, confidence)
		VALUES ((CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Dar_es_Salaam')::date + 1, '23:00:00', 'Test', 'Home FC', 'Away FC', 'upcoming', 'high')
		RETURNING id
	`).Scan(&matchID)
	if err != nil {
		t.Fatalf("seed match: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO match_markets (match_id, market) VALUES ($1, 'ft_over_0_5')`, matchID); err != nil {
		t.Fatalf("seed market: %v", err)
	}

	userTok, err := s.issueToken(userID, phone, false)
	if err != nil {
		t.Fatal(err)
	}

	// place bet
	betReq := httptest.NewRequest("POST", "/api/v1/bets", bytes.NewBufferString(
		`{"match_id":"`+matchID+`","market":"ft_over_0_5","stake_cents":50000}`,
	))
	betReq.Header.Set("Authorization", "Bearer "+userTok)
	betReq.Header.Set("Content-Type", "application/json")
	betReq = betReq.WithContext(context.WithValue(betReq.Context(), userCtxKey, &AuthUser{ID: userID, Phone: phone}))
	w := doReq(s.handlePlaceBet, betReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("place bet code=%d body=%s", w.Code, w.Body.String())
	}

	var balanceAfterBet, pendingBets int64
	if err := pool.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1`, userID).Scan(&balanceAfterBet); err != nil {
		t.Fatal(err)
	}
	if balanceAfterBet != 50000 {
		t.Fatalf("balance after bet=%d want 50000", balanceAfterBet)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bets WHERE match_id=$1 AND status='pending'`, matchID).Scan(&pendingBets); err != nil {
		t.Fatal(err)
	}
	if pendingBets != 1 {
		t.Fatalf("pending bets=%d want 1", pendingBets)
	}

	// settle: 1-0 => ft_over_0_5 wins, payout 50000*15000/10000 = 75000
	settleBody := map[string]interface{}{
		"home_goals": 1, "away_goals": 0,
		"source": "test", "reason": "integration settle",
	}
	r := reqWithChiParam("POST", "/api/v1/admin/matches/"+matchID+"/settle", settleBody, "id", matchID, nil)
	w = doReq(s.handleAdminSettleMatch, r)
	if w.Code != http.StatusOK {
		t.Fatalf("settle code=%d body=%s", w.Code, w.Body.String())
	}
	settleOut := jsonMap(t, w)
	if corr, _ := settleOut["correction"].(bool); corr {
		t.Fatalf("expected initial settlement, got correction=%v body=%v", corr, settleOut)
	}
	if settleOut["wins"].(float64) != 1 || settleOut["losses"].(float64) != 0 {
		t.Fatalf("unexpected settle counts: %v", settleOut)
	}
	var balAfterWin int64
	if err := pool.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1`, userID).Scan(&balAfterWin); err != nil {
		t.Fatal(err)
	}
	if balAfterWin != 125000 { // 50000 remaining + 75000 payout
		t.Fatalf("balance after win=%d want 125000", balAfterWin)
	}

	// public feed must expose the settled score so the UI can show win/loss ticks
	r2 := httptest.NewRequest("GET", "/api/v1/matches", nil)
	w2 := doReq(s.handleListMatches, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list matches code=%d", w2.Code)
	}
	var feed struct {
		Matches []struct {
			ID      string `json:"id"`
			Settled bool   `json:"settled"`
			HomeGoals *int `json:"home_goals"`
			AwayGoals *int `json:"away_goals"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &feed); err != nil {
		t.Fatalf("feed decode: %v", err)
	}
	var found bool
	for _, fm := range feed.Matches {
		if fm.ID == matchID {
			found = true
			if !fm.Settled || fm.HomeGoals == nil || *fm.HomeGoals != 1 || fm.AwayGoals == nil || *fm.AwayGoals != 0 {
				t.Fatalf("settled feed row wrong: %+v", fm)
			}
		}
	}
	if !found {
		t.Fatal("settled match missing from public feed")
	}

	// correct: 0-0 => should reverse win and result in loss (no payout), balance back to 50000
	correctBody := map[string]interface{}{
		"home_goals": 0, "away_goals": 0,
		"source": "test", "reason": "integration correction", "correct": true,
	}
	r = reqWithChiParam("POST", "/api/v1/admin/matches/"+matchID+"/settle", correctBody, "id", matchID, nil)
	w = doReq(s.handleAdminSettleMatch, r)
	if w.Code != http.StatusOK {
		t.Fatalf("correct code=%d body=%s", w.Code, w.Body.String())
	}
	correctOut := jsonMap(t, w)
	if corr, _ := correctOut["correction"].(bool); !corr {
		t.Fatalf("expected correction response: %v", correctOut)
	}
	var balAfterCorrection int64
	var betStatus string
	var payoutCents int64
	if err := pool.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1`, userID).Scan(&balAfterCorrection); err != nil {
		t.Fatal(err)
	}
	if balAfterCorrection != 50000 {
		t.Fatalf("balance after correction=%d want 50000", balAfterCorrection)
	}
	if err := pool.QueryRow(ctx, `SELECT status, payout_cents FROM bets WHERE match_id=$1 AND user_id=$2`, matchID, userID).Scan(&betStatus, &payoutCents); err != nil {
		t.Fatal(err)
	}
	if betStatus != "loss" || payoutCents != 0 {
		t.Fatalf("bet after correction status=%s payout=%d", betStatus, payoutCents)
	}
	var archiveRows int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM results_archive WHERE match_id=$1`, matchID).Scan(&archiveRows); err != nil {
		t.Fatal(err)
	}
	if archiveRows < 2 {
		t.Fatalf("expected correction archive rows >=2 got %d", archiveRows)
	}
}
