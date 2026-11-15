package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type registerRequest struct {
	Nickname      string `json:"nickname"`
	Phone         string `json:"phone"`
	OfficialName  string `json:"official_name"`
	Password      string `json:"password"`
	AcceptedTerms bool   `json:"accepted_terms"`
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, " ", "")
	p = strings.ReplaceAll(p, "-", "")
	if strings.HasPrefix(p, "0") {
		p = "+255" + p[1:]
	} else if strings.HasPrefix(p, "7") || strings.HasPrefix(p, "6") {
		p = "+255" + p
	} else if !strings.HasPrefix(p, "+") {
		p = "+" + p
	}
	return p
}

var phoneRe = regexp.MustCompile(`^\+255[0-9]{9}$`)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.Phone = normalizePhone(req.Phone)
	req.OfficialName = strings.TrimSpace(req.OfficialName)

	if req.Nickname == "" || req.Phone == "" || req.OfficialName == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing fields"})
		return
	}
	if !phoneRe.MatchString(req.Phone) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid phone"})
		return
	}
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short"})
		return
	}
	if !req.AcceptedTerms {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "terms not accepted"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash failed"})
		return
	}

	ctx := r.Context()
	var userID string
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO users (nickname, phone, official_name, password_hash, status)
		VALUES ($1, $2, $3, $4, 'inactive')
		RETURNING id
	`, req.Nickname, req.Phone, req.OfficialName, string(hash)).Scan(&userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "phone already registered"})
		return
	}

	// record consent
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO consent_log (user_id, notice_version, accepted_at) VALUES ($1, 1, NOW())
	`, userID)

	tok, err := s.issueToken(userID, req.Phone, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token": tok,
		"user":  map[string]string{"id": userID, "phone": req.Phone, "nickname": req.Nickname, "status": "inactive"},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	req.Phone = normalizePhone(req.Phone)

	ctx := r.Context()
	var userID, hash, nickname, status string
	err := s.Pool.QueryRow(ctx, `
		SELECT id, password_hash, nickname, status FROM users WHERE phone = $1
	`, req.Phone).Scan(&userID, &hash, &nickname, &status)
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if status == "banned" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "account banned"})
		return
	}

	tok, err := s.issueToken(userID, req.Phone, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": tok,
		"user":  map[string]string{"id": userID, "phone": req.Phone, "nickname": nickname, "status": status},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Stateless JWT: client discards token. Could add a denylist in Redis later.
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	var nickname, phone, status, officialName string
	var balance int64
	var accessExpiresAt *string
	err := s.Pool.QueryRow(ctx, `
		SELECT nickname, phone, status, official_name, balance_cents,
		       to_char(access_expires_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI')
		FROM users WHERE id = $1
	`, u.ID).Scan(&nickname, &phone, &status, &officialName, &balance, &accessExpiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":                u.ID,
		"nickname":          nickname,
		"phone":             phone,
		"status":            status,
		"official_name":     officialName,
		"balance_cents":     balance,
		"access_expires_at": accessExpiresAt,
	})
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Password   string `json:"password"`
		ProfilePic string `json:"profile_pic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	ctx := r.Context()
	if body.Password != "" {
		if len(body.Password) < 6 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hash failed"})
			return
		}
		_, err = s.Pool.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, string(hash), u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}
	if body.ProfilePic != "" {
		// size check done client-side (<10KB); store as-is (base64 or URL)
		_, err := s.Pool.Exec(ctx, `UPDATE users SET profile_pic = $1, updated_at = NOW() WHERE id = $2`, body.ProfilePic, u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	// placeholder: return empty list
	writeJSON(w, http.StatusOK, map[string]interface{}{"bets": []interface{}{}})
}

func (s *Server) handlePlaceBet(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented yet"})
}
