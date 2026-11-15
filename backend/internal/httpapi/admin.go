package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAdminLogin authenticates against credentials held only in environment
// variables. Admin accounts are never stored in the database.
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.AdminUser == "" || s.Cfg.AdminPass == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "admin credentials not configured",
		})
		return
	}

	var req adminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	userOK := subtle.ConstantTimeCompare([]byte(req.Username), []byte(s.Cfg.AdminUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.Cfg.AdminPass)) == 1
	if !userOK || !passOK {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	tok, err := s.issueToken("admin", "", true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": tok,
		"admin": map[string]string{"username": s.Cfg.AdminUser},
	})
}

func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username": s.Cfg.AdminUser,
		"env":      s.Cfg.Env,
	})
}

// adminName returns a stable label for audit rows.
func adminName(r *http.Request) string {
	if u := UserFromCtx(r); u != nil && u.ID != "" {
		return "admin"
	}
	return "admin"
}

// handleAdminListUsers supports the Users & Financial Status centre.
// Query params: q (phone/nickname search), status, limit, offset.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	limit := atoiDefault(r.URL.Query().Get("limit"), 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT id, nickname, phone, official_name, status, balance_cents,
		       to_char(access_expires_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM users
		WHERE ($1 = '' OR phone ILIKE '%' || $1 || '%' OR nickname ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, q, status, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type userRow struct {
		ID              string  `json:"id"`
		Nickname        string  `json:"nickname"`
		Phone           string  `json:"phone"`
		OfficialName    string  `json:"official_name"`
		Status          string  `json:"status"`
		BalanceCents    int64   `json:"balance_cents"`
		AccessExpiresAt *string `json:"access_expires_at"`
		CreatedAt       string  `json:"created_at"`
	}

	out := []userRow{}
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Nickname, &u.Phone, &u.OfficialName, &u.Status,
			&u.BalanceCents, &u.AccessExpiresAt, &u.CreatedAt); err != nil {
			continue
		}
		out = append(out, u)
	}

	var total int
	_ = s.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE ($1 = '' OR phone ILIKE '%' || $1 || '%' OR nickname ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR status = $2)
	`, q, status).Scan(&total)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// handleAdminUpdateUserStatus performs manual activation, suspension or ban.
// Body: {status, days, reason}
func (s *Server) handleAdminUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID := chiURLParam(r, "id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
		return
	}

	var body struct {
		Status string `json:"status"`
		Days   int    `json:"days"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	switch body.Status {
	case "active", "inactive", "suspended", "banned":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}

	ctx := r.Context()
	var oldStatus string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&oldStatus); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	// Activation grants an access window; other statuses leave the expiry alone.
	if body.Status == "active" {
		days := body.Days
		if days <= 0 {
			days = 30
		}
		_, err := s.Pool.Exec(ctx, `
			UPDATE users
			SET status = 'active',
			    access_expires_at = NOW() + make_interval(days => $1),
			    updated_at = NOW()
			WHERE id = $2
		`, days, userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
		s.notifyAndPush([]string{userID}, catAccount, "Account activated",
			fmt.Sprintf("Your access is active for the next %d days. Betting is open again.", days), "/profile")
	} else {
		_, err := s.Pool.Exec(ctx, `
			UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2
		`, body.Status, userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}

	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO user_status_log (user_id, old_status, new_status, reason, reviewer)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, oldStatus, body.Status, body.Reason, adminName(r))

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     body.Status,
		"old_status": oldStatus,
	})
}
