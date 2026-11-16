package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/betva/backend/internal/config"
)

func testServer() *Server {
	return &Server{
		Cfg: &config.Config{
			Env:              "test",
			JWTSecret:        "test-secret-0123456789",
			AdminUser:        "admin",
			AdminPass:        "secret",
			StakeTiers:       []int64{50000, 100000},
			PayoutMultiplier: 15000,
			VAPIDPublicKey:   "pub",
			VAPIDPrivateKey:  "priv",
			PushDailyCap:     2,
			QuietStartHour:   22,
			QuietEndHour:     7,
		},
	}
}

func doJSON(h http.HandlerFunc, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode json: %v body=%s", err, w.Body.String())
	}
	return out
}

func TestHandleHealthAndPing(t *testing.T) {
	s := testServer()
	w := doJSON(s.handleHealth, "GET", "/health", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health code=%d", w.Code)
	}
	w = doJSON(s.handlePing, "GET", "/api/v1/ping", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ping code=%d", w.Code)
	}
	got := decode(t, w)
	if v, _ := got["pong"].(bool); !v {
		t.Fatalf("pong=%v", got)
	}
}

func TestHandleConfig(t *testing.T) {
	s := testServer()
	w := doJSON(s.handleConfig, "GET", "/api/v1/config", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	got := decode(t, w)
	if got["payout_multiplier_bp"].(float64) != 15000 {
		t.Fatalf("multiplier=%v", got["payout_multiplier_bp"])
	}
	if got["push_enabled"] != true {
		t.Fatalf("push_enabled=%v", got["push_enabled"])
	}
}

func TestHandleAdminLogin(t *testing.T) {
	s := testServer()

	// bad json
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/login", bytes.NewBufferString("{"))
	s.handleAdminLogin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json code=%d", w.Code)
	}

	// wrong creds
	w = doJSON(s.handleAdminLogin, "POST", "/api/v1/admin/login", map[string]string{
		"username": "admin", "password": "nope",
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong creds code=%d", w.Code)
	}

	// success
	w = doJSON(s.handleAdminLogin, "POST", "/api/v1/admin/login", map[string]string{
		"username": "admin", "password": "secret",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("success code=%d body=%s", w.Code, w.Body.String())
	}
	got := decode(t, w)
	tok, _ := got["token"].(string)
	if tok == "" {
		t.Fatal("expected token")
	}
	claims, err := s.parseToken(tok)
	if err != nil || claims == nil || !claims.IsAdmin || claims.Subject != "admin" {
		t.Fatalf("claims err=%v claims=%+v", err, claims)
	}
}

func TestHandleAdminLoginUnconfigured(t *testing.T) {
	s := testServer()
	s.Cfg.AdminUser = ""
	w := doJSON(s.handleAdminLogin, "POST", "/api/v1/admin/login", map[string]string{
		"username": "admin", "password": "secret",
	}, nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	s := testServer()
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromCtx(r)
		if u == nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": u.ID})
	}))

	// missing token
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing code=%d", w.Code)
	}

	// invalid token
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer bad")
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid code=%d", w.Code)
	}

	// valid token
	tok, err := s.issueToken("user-1", "+255712345678", false)
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid code=%d body=%s", w.Code, w.Body.String())
	}
	if got := decode(t, w)["id"]; got != "user-1" {
		t.Fatalf("id=%v", got)
	}
}

func TestAdminMiddleware(t *testing.T) {
	s := testServer()
	protected := s.adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))

	userTok, _ := s.issueToken("user-1", "+255712345678", false)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+userTok)
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user code=%d", w.Code)
	}

	adminTok, _ := s.issueToken("admin", "", true)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCORSMiddleware(t *testing.T) {
	h := CORSMiddleware([]string{"http://localhost:4321"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/ping", nil)
	req.Header.Set("Origin", "http://localhost:4321")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight code=%d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:4321" {
		t.Fatalf("allow origin=%q", got)
	}
}
