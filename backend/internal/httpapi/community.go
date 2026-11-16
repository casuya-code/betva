package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxPostBody = 500
const autoHideAfterReports = 3

// handleCommunityFeed returns a cursor-paginated feed of visible posts.
// Infinite scroll is allowed here; the client stops when next_before is null.
func (s *Server) handleCommunityFeed(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	var before *time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "before must be RFC3339"})
			return
		}
		before = &t
	}

	rows, err := s.Pool.Query(r.Context(), `
		SELECT p.id, p.body, COALESCE(p.media_url, ''), COALESCE(p.media_type, ''), p.media_bytes,
		       to_char(p.created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       p.created_at, p.user_id, u.nickname, COALESCE(u.profile_pic, ''),
		       (p.user_id = $1),
		       EXISTS (SELECT 1 FROM post_reports pr WHERE pr.post_id = p.id AND pr.reporter_id = $1),
		       (SELECT COUNT(*) FROM post_reports pr WHERE pr.post_id = p.id),
		       (SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id = p.id AND pr.reaction = 'like'),
		       (SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id = p.id AND pr.reaction = 'love'),
		       (SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id = p.id AND pr.reaction = 'haha'),
		       COALESCE((SELECT pr.reaction FROM post_reactions pr WHERE pr.post_id = p.id AND pr.user_id = $1), '')
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.status = 'visible'
		  AND ($2::timestamptz IS NULL OR p.created_at < $2)
		ORDER BY p.created_at DESC
		LIMIT $3
	`, u.ID, before, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type post struct {
		ID          string    `json:"id"`
		Body        string    `json:"body"`
		MediaURL    string    `json:"media_url"`
		MediaType   string    `json:"media_type"`
		MediaBytes  int64     `json:"media_bytes"`
		CreatedAt   string    `json:"created_at"`
		CreatedRaw  time.Time `json:"-"`
		UserID      string    `json:"user_id"`
		Nickname    string    `json:"nickname"`
		ProfilePic  string    `json:"profile_pic"`
		IsMine      bool      `json:"is_mine"`
		Reported    bool      `json:"reported"`
		ReportCount int       `json:"report_count"`
		Likes       int       `json:"likes"`
		Loves       int       `json:"loves"`
		Hahas       int       `json:"hahas"`
		MyReaction  string    `json:"my_reaction"`
	}

	out := []post{}
	for rows.Next() {
		var p post
		if err := rows.Scan(&p.ID, &p.Body, &p.MediaURL, &p.MediaType, &p.MediaBytes,
			&p.CreatedAt, &p.CreatedRaw, &p.UserID, &p.Nickname, &p.ProfilePic,
			&p.IsMine, &p.Reported, &p.ReportCount,
			&p.Likes, &p.Loves, &p.Hahas, &p.MyReaction); err != nil {
			continue
		}
		out = append(out, p)
	}

	var next *string
	if len(out) == limit {
		n := out[len(out)-1].CreatedRaw.UTC().Format(time.RFC3339Nano)
		next = &n
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"posts":       out,
		"next_before": next,
	})
}

// handleCreatePost publishes a text and/or media post.
func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if !s.requireActive(w, r, u) {
		return
	}

	var body struct {
		Body       string `json:"body"`
		MediaURL   string `json:"media_url"`
		MediaType  string `json:"media_type"`
		MediaBytes int64  `json:"media_bytes"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	body.Body = strings.TrimSpace(body.Body)
	body.MediaURL = strings.TrimSpace(body.MediaURL)

	if utf8.RuneCountInString(body.Body) > maxPostBody {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "post is longer than 500 characters"})
		return
	}
	if body.Body == "" && body.MediaURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "write something or attach media"})
		return
	}
	if body.MediaURL != "" {
		// Only media issued by our own upload endpoint is accepted, which stops
		// arbitrary remote images from turning the feed into a tracking surface.
		if !strings.HasPrefix(body.MediaURL, "/media/") || strings.Contains(body.MediaURL, "..") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media url"})
			return
		}
		if body.MediaType != "image" && body.MediaType != "video" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media type"})
			return
		}
	}

	var id string
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO posts (user_id, body, media_url, media_type, media_bytes)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5)
		RETURNING id
	`, u.ID, body.Body, body.MediaURL, body.MediaType, body.MediaBytes).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not publish post"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleReactPost stores one real reaction per user per post. Clicking the
// same reaction again removes it; clicking a different one changes it.
func (s *Server) handleReactPost(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	postID := chiURLParam(r, "id")
	if postID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing post id"})
		return
	}

	var body struct {
		Reaction string `json:"reaction"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	switch body.Reaction {
	case "like", "love", "haha":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reaction"})
		return
	}

	ctx := r.Context()
	var visible bool
	if err := s.Pool.QueryRow(ctx, `SELECT status = 'visible' FROM posts WHERE id = $1`, postID).Scan(&visible); err != nil || !visible {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "post not found"})
		return
	}

	var current string
	_ = s.Pool.QueryRow(ctx, `
		SELECT reaction FROM post_reactions WHERE post_id = $1 AND user_id = $2
	`, postID, u.ID).Scan(&current)

	if current == body.Reaction {
		_, err := s.Pool.Exec(ctx, `DELETE FROM post_reactions WHERE post_id = $1 AND user_id = $2`, postID, u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	} else {
		_, err := s.Pool.Exec(ctx, `
			INSERT INTO post_reactions (post_id, user_id, reaction) VALUES ($1, $2, $3)
			ON CONFLICT (post_id, user_id) DO UPDATE SET reaction = EXCLUDED.reaction, created_at = NOW()
		`, postID, u.ID, body.Reaction)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
			return
		}
	}

	var likes, loves, hahas int
	var mine string
	if err := s.Pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE reaction = 'like'),
		  COUNT(*) FILTER (WHERE reaction = 'love'),
		  COUNT(*) FILTER (WHERE reaction = 'haha'),
		  COALESCE(MAX(reaction) FILTER (WHERE user_id = $2), '')
		FROM post_reactions WHERE post_id = $1
	`, postID, u.ID).Scan(&likes, &loves, &hahas, &mine); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"likes": likes, "loves": loves, "hahas": hahas, "my_reaction": mine,
	})
}

// handleDeleteOwnPost soft-deletes the caller's post. Nothing is physically
// removed: moderators need the record if a report is disputed.
func (s *Server) handleDeleteOwnPost(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if !s.requireActive(w, r, u) {
		return
	}
	id := chiURLParam(r, "id")
	tag, err := s.Pool.Exec(r.Context(), `
		UPDATE posts SET status = 'deleted', updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status <> 'deleted'
	`, id, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "post not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleReportPost records a report from the caller. Three distinct reports
// auto-hide a post pending review; that is a guardrail, not a verdict.
func (s *Server) handleReportPost(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if !s.requireActive(w, r, u) {
		return
	}
	postID := chiURLParam(r, "id")

	var body struct {
		Reason string `json:"reason"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		body.Reason = "no reason given"
	}
	if utf8.RuneCountInString(body.Reason) > 280 {
		body.Reason = string([]rune(body.Reason)[:280])
	}

	ctx := r.Context()
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM posts WHERE id = $1 AND status = 'visible')`, postID).Scan(&exists); err != nil || !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "post not found"})
		return
	}

	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO post_reports (post_id, reporter_id, reason)
		VALUES ($1, $2, $3)
		ON CONFLICT (post_id, reporter_id) DO NOTHING
	`, postID, u.ID, body.Reason)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	var reportCount int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM post_reports WHERE post_id = $1`, postID).Scan(&reportCount)

	autoHidden := false
	if reportCount >= autoHideAfterReports {
		tag2, err := s.Pool.Exec(ctx, `
			UPDATE posts SET status = 'hidden', moderation_reason = 'auto-hidden after reports', updated_at = NOW()
			WHERE id = $1 AND status = 'visible'
		`, postID)
		if err == nil && tag2.RowsAffected() > 0 {
			autoHidden = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reported":      tag.RowsAffected() > 0,
		"report_count":  reportCount,
		"auto_hidden":   autoHidden,
		"already_filed": tag.RowsAffected() == 0,
	})
}

// handleMediaUpload accepts a small image or short video and stores it on disk.
// Production swaps this for Cloudflare Images / Stream; the size limits and
// "no autoplay" rule stay the same.
func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if !s.requireActive(w, r, u) {
		return
	}

	maxBytes := s.Cfg.MaxVideoBytes
	if s.Cfg.MaxImageBytes > maxBytes {
		maxBytes = s.Cfg.MaxImageBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024)

	if err := r.ParseMultipartForm(maxBytes + 1024*1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is too large"})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	defer file.Close()

	// Read the whole upload into memory (max 3 MB) so we can sniff, size-check,
	// and stream it to either disk or R2 without juggling partial readers.
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read file"})
		return
	}
	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	if int64(len(data)) > maxBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is too large"})
		return
	}

	// Sniff from the bytes, never trust the client's Content-Type.
	mime := http.DetectContentType(data)
	if len(data) < 512 {
		// DetectContentType handles short slices; keep data as-is.
	}

	ext, mediaType := "", ""
	switch mime {
	case "image/jpeg":
		ext, mediaType = ".jpg", "image"
	case "image/png":
		ext, mediaType = ".png", "image"
	case "image/webp":
		ext, mediaType = ".webp", "image"
	case "image/avif":
		ext, mediaType = ".avif", "image"
	case "image/gif":
		ext, mediaType = ".gif", "image"
	case "video/mp4":
		ext, mediaType = ".mp4", "video"
	case "video/webm":
		ext, mediaType = ".webm", "video"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported file type"})
		return
	}

	limit := s.Cfg.MaxImageBytes
	if mediaType == "video" {
		limit = s.Cfg.MaxVideoBytes
	}
	if int64(len(data)) > limit {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":      "file is too large",
			"max_bytes":  limit,
			"media_type": mediaType,
		})
		return
	}

	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "media storage unavailable"})
		return
	}
	name := hex.EncodeToString(raw) + ext

	url, err := s.Media.store(r.Context(), name, data, mime)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "media storage unavailable"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"url":        url,
		"media_type": mediaType,
		"bytes":      len(data),
		"by":         u.ID,
	})
}

// ---------------------------------------------------------------------------
// Admin moderation
// ---------------------------------------------------------------------------

// handleAdminCommunityPosts lists posts for moderation.
func (s *Server) handleAdminCommunityPosts(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", "visible", "hidden", "deleted", "reported":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	ctx := r.Context()
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.body, COALESCE(p.media_url, ''), COALESCE(p.media_type, ''), p.status,
		       COALESCE(p.moderation_reason, ''), p.user_id, u.nickname, u.phone,
		       to_char(p.created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       (SELECT COUNT(*) FROM post_reports pr WHERE pr.post_id = p.id),
		       (SELECT COUNT(*) FROM post_reports pr WHERE pr.post_id = p.id AND pr.resolved_at IS NULL)
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE ($1 = '' OR p.status = $1 OR ($1 = 'reported' AND EXISTS (
			SELECT 1 FROM post_reports pr WHERE pr.post_id = p.id AND pr.resolved_at IS NULL)))
		ORDER BY p.created_at DESC
		LIMIT $2
	`, status, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type row struct {
		ID               string `json:"id"`
		Body             string `json:"body"`
		MediaURL         string `json:"media_url"`
		MediaType        string `json:"media_type"`
		Status           string `json:"status"`
		ModerationReason string `json:"moderation_reason"`
		UserID           string `json:"user_id"`
		Nickname         string `json:"nickname"`
		Phone            string `json:"phone"`
		CreatedAt        string `json:"created_at"`
		ReportCount      int    `json:"report_count"`
		OpenReports      int    `json:"open_reports"`
	}
	out := []row{}
	for rows.Next() {
		var p row
		if err := rows.Scan(&p.ID, &p.Body, &p.MediaURL, &p.MediaType, &p.Status,
			&p.ModerationReason, &p.UserID, &p.Nickname, &p.Phone, &p.CreatedAt,
			&p.ReportCount, &p.OpenReports); err == nil {
			out = append(out, p)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"posts": out, "count": len(out)})
}

// handleAdminModeratePost hides, restores, or deletes a post and resolves its
// open reports. Every action records the reviewer and reason.
func (s *Server) handleAdminModeratePost(w http.ResponseWriter, r *http.Request) {
	id := chiURLParam(r, "id")
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	switch body.Status {
	case "visible", "hidden", "deleted":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be visible, hidden, or deleted"})
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)

	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE posts
		SET status = $1, moderation_reason = NULLIF($2, ''), moderated_by = $3, updated_at = NOW()
		WHERE id = $4
	`, body.Status, body.Reason, adminName(r), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "post not found"})
		return
	}

	// Every decision closes the open reports, including a restore. Otherwise a
	// reviewed post would sit in the "reported" queue forever.
	resolution := body.Status
	if body.Status == "visible" {
		resolution = "dismissed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE post_reports SET resolved_at = NOW(), resolution = $1
		WHERE post_id = $2 AND resolved_at IS NULL
	`, resolution, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}
