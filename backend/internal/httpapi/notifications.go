package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	catMatchStart    = "match_start"
	catResults       = "results"
	catAccount       = "account"
	catAnnouncements = "announcements"
	catCommunity     = "community"

	eatOffsetSeconds = 3 * 60 * 60
)

var eatLocation = time.FixedZone("EAT", eatOffsetSeconds)

func validCategory(c string) bool {
	switch c {
	case catMatchStart, catResults, catAccount, catAnnouncements, catCommunity:
		return true
	}
	return false
}

// prefsColumn maps a category to its mute column. The map is fixed, so the
// column name can never come from user input.
func prefsColumn(category string) string {
	switch category {
	case catMatchStart:
		return "match_start"
	case catResults:
		return "results"
	case catAccount:
		return "account"
	case catAnnouncements:
		return "announcements"
	case catCommunity:
		return "community"
	}
	return "announcements"
}

// insertNotification writes an inbox row. Push delivery is a separate,
// best-effort step so a failed push never loses the message.
func (s *Server) insertNotification(ctx context.Context, userID, category, title, body, link string) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, category, title, body, link)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		RETURNING id
	`, userID, category, title, body, link).Scan(&id)
	return id, err
}

// notifyAndPush is the fire-and-forget path used by settlement and activation.
func (s *Server) notifyAndPush(userIDs []string, category, title, body, link string) {
	if len(userIDs) == 0 {
		return
	}
	go func(ids []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		for _, uid := range ids {
			_, _ = s.insertNotification(ctx, uid, category, title, body, link)
		}
		s.dispatchPendingPush(ids)
	}(append([]string(nil), userIDs...))
}

// dispatchPendingPush delivers unsent notifications, enforcing quiet hours and
// the per-user daily cap. Failures are recorded on the row, never retried in a
// loop that could wake a user twice.
func (s *Server) dispatchPendingPush(userIDs []string) {
	if len(userIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now().In(eatLocation)
	quiet := now.Hour() >= s.Cfg.QuietStartHour || now.Hour() < s.Cfg.QuietEndHour

	rows, err := s.Pool.Query(ctx, `
		SELECT n.id, n.user_id, n.category, n.title, n.body, COALESCE(n.link, '')
		FROM notifications n
		WHERE n.user_id = ANY($1::uuid[])
		  AND n.push_sent_at IS NULL
		  AND n.push_error IS NULL
		ORDER BY n.created_at
		LIMIT 500
	`, userIDs)
	if err != nil {
		return
	}
	type notice struct {
		ID, UserID, Category, Title, Body, Link string
	}
	var pending []notice
	for rows.Next() {
		var n notice
		if err := rows.Scan(&n.ID, &n.UserID, &n.Category, &n.Title, &n.Body, &n.Link); err == nil {
			pending = append(pending, n)
		}
	}
	rows.Close()

	for _, n := range pending {
		if !s.Cfg.PushEnabled() {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = 'push not configured' WHERE id = $1`, n.ID)
			continue
		}
		if quiet {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = 'quiet hours' WHERE id = $1`, n.ID)
			continue
		}

		var muted bool
		_ = s.Pool.QueryRow(ctx, `
			SELECT COALESCE((
				SELECT NOT `+prefsColumn(n.Category)+` FROM notification_prefs WHERE user_id = $1
			), FALSE)
		`, n.UserID).Scan(&muted)
		if muted {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = 'muted by user' WHERE id = $1`, n.ID)
			continue
		}

		var sentToday int
		_ = s.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM push_delivery_log
			WHERE user_id = $1
			  AND (sent_at AT TIME ZONE 'Africa/Dar_es_Salaam')::date
			      = (NOW() AT TIME ZONE 'Africa/Dar_es_Salaam')::date
		`, n.UserID).Scan(&sentToday)
		if s.Cfg.PushDailyCap > 0 && sentToday >= s.Cfg.PushDailyCap {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = 'daily cap reached' WHERE id = $1`, n.ID)
			continue
		}

		subRows, err := s.Pool.Query(ctx, `
			SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = $1
		`, n.UserID)
		if err != nil {
			continue
		}
		type sub struct {
			ID, Endpoint, P256DH, Auth string
		}
		var subs []sub
		for subRows.Next() {
			var x sub
			if err := subRows.Scan(&x.ID, &x.Endpoint, &x.P256DH, &x.Auth); err == nil {
				subs = append(subs, x)
			}
		}
		subRows.Close()
		if len(subs) == 0 {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = 'no subscription' WHERE id = $1`, n.ID)
			continue
		}

		payload, _ := json.Marshal(map[string]string{
			"title": n.Title,
			"body":  n.Body,
			"link":  n.Link,
			"tag":   n.Category,
		})

		delivered := false
		var lastErr string
		for _, sub := range subs {
			resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys: webpush.Keys{
					P256dh: sub.P256DH,
					Auth:   sub.Auth,
				},
			}, &webpush.Options{
				Subscriber:      s.Cfg.VAPIDSubject,
				VAPIDPublicKey:  s.Cfg.VAPIDPublicKey,
				VAPIDPrivateKey: s.Cfg.VAPIDPrivateKey,
				TTL:             3600,
			})
			if err != nil {
				lastErr = err.Error()
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
				_, _ = s.Pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, sub.ID)
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				delivered = true
			}
		}

		if delivered {
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_sent_at = NOW() WHERE id = $1`, n.ID)
			_, _ = s.Pool.Exec(ctx, `
				INSERT INTO push_delivery_log (user_id, notification_id) VALUES ($1, $2)
			`, n.UserID, n.ID)
		} else {
			if lastErr == "" {
				lastErr = "push failed"
			}
			_, _ = s.Pool.Exec(ctx, `UPDATE notifications SET push_error = $1 WHERE id = $2`, lastErr, n.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// User inbox and preferences
// ---------------------------------------------------------------------------

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, category, title, body, COALESCE(link, ''),
		       to_char(created_at AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       (read_at IS NOT NULL)
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, u.ID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type item struct {
		ID        string `json:"id"`
		Category  string `json:"category"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Link      string `json:"link"`
		CreatedAt string `json:"created_at"`
		Read      bool   `json:"read"`
	}
	out := []item{}
	for rows.Next() {
		var n item
		if err := rows.Scan(&n.ID, &n.Category, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.Read); err == nil {
			out = append(out, n)
		}
	}

	var unread int
	_ = s.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, u.ID).Scan(&unread)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": out,
		"unread":        unread,
	})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chiURLParam(r, "id")
	_, err := s.Pool.Exec(r.Context(), `
		UPDATE notifications SET read_at = NOW() WHERE id = $1 AND user_id = $2 AND read_at IS NULL
	`, id, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	_, err := s.Pool.Exec(r.Context(), `UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND read_at IS NULL`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

func (s *Server) handleGetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	_, _ = s.Pool.Exec(ctx, `INSERT INTO notification_prefs (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID)

	var p struct {
		MatchStart    bool `json:"match_start"`
		Results       bool `json:"results"`
		Account       bool `json:"account"`
		Announcements bool `json:"announcements"`
		Community     bool `json:"community"`
	}
	if err := s.Pool.QueryRow(ctx, `
		SELECT match_start, results, account, announcements, community
		FROM notification_prefs WHERE user_id = $1
	`, u.ID).Scan(&p.MatchStart, &p.Results, &p.Account, &p.Announcements, &p.Community); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		MatchStart    *bool `json:"match_start"`
		Results       *bool `json:"results"`
		Account       *bool `json:"account"`
		Announcements *bool `json:"announcements"`
		Community     *bool `json:"community"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	ctx := r.Context()
	_, _ = s.Pool.Exec(ctx, `INSERT INTO notification_prefs (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID)

	set := func(col string, val *bool) {
		if val == nil {
			return
		}
		_, _ = s.Pool.Exec(ctx, `UPDATE notification_prefs SET `+col+` = $1, updated_at = NOW() WHERE user_id = $2`, *val, u.ID)
	}
	set("match_start", body.MatchStart)
	set("results", body.Results)
	set("account", body.Account)
	set("announcements", body.Announcements)
	set("community", body.Community)

	s.handleGetNotificationPrefs(w, r)
}

// ---------------------------------------------------------------------------
// Push subscriptions
// ---------------------------------------------------------------------------

func (s *Server) handleSubscribePush(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		P256DH   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	body.Endpoint = strings.TrimSpace(body.Endpoint)
	if body.Endpoint == "" || body.P256DH == "" || body.Auth == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint, p256dh, and auth are required"})
		return
	}
	if !strings.HasPrefix(body.Endpoint, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid endpoint"})
		return
	}

	_, err := s.Pool.Exec(r.Context(), `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh,
		    auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent, last_seen_at = NOW()
	`, u.ID, body.Endpoint, body.P256DH, body.Auth, r.UserAgent())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

func (s *Server) handleUnsubscribePush(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2`, body.Endpoint, u.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// ---------------------------------------------------------------------------
// Self-exclusion
// ---------------------------------------------------------------------------

func (s *Server) handleGetSelfExclusion(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var until *string
	var hours *int
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT to_char(until AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'), duration_hours
		FROM self_exclusions
		WHERE user_id = $1 AND until > NOW()
		ORDER BY until DESC
		LIMIT 1
	`, u.ID).Scan(&until, &hours)

	if until == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"active": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":         true,
		"until":          until,
		"duration_hours": hours,
	})
}

// handleSetSelfExclusion locks betting for 24h / 7d / 30d. There is no endpoint
// to shorten it: that is the point of self-exclusion.
func (s *Server) handleSetSelfExclusion(w http.ResponseWriter, r *http.Request) {
	u := UserFromCtx(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Hours int `json:"hours"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if body.Hours != 24 && body.Hours != 168 && body.Hours != 720 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours must be 24, 168, or 720"})
		return
	}

	ctx := r.Context()
	var existing *string
	_ = s.Pool.QueryRow(ctx, `
		SELECT to_char(until AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI')
		FROM self_exclusions WHERE user_id = $1 AND until > NOW()
		ORDER BY until DESC LIMIT 1
	`, u.ID).Scan(&existing)
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "a self-exclusion is already active and cannot be shortened",
			"until": existing,
		})
		return
	}

	var until string
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO self_exclusions (user_id, until, duration_hours)
		VALUES ($1, NOW() + make_interval(hours => $2), $2)
		RETURNING to_char(until AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI')
	`, u.ID, body.Hours).Scan(&until)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"active":         true,
		"until":          until,
		"duration_hours": body.Hours,
	})
}

// ---------------------------------------------------------------------------
// Admin broadcasts
// ---------------------------------------------------------------------------

func (s *Server) handleAdminSendNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Link     string `json:"link"`
		Category string `json:"category"`
		Audience string `json:"audience"`
	}
	if err := jsonDecode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Body = strings.TrimSpace(body.Body)
	body.Link = strings.TrimSpace(body.Link)
	body.Category = strings.TrimSpace(body.Category)

	if body.Title == "" || utf8.RuneCountInString(body.Title) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required (max 120 characters)"})
		return
	}
	if utf8.RuneCountInString(body.Body) > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is too long (max 500 characters)"})
		return
	}
	if !validCategory(body.Category) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
		return
	}
	if body.Link != "" && !strings.HasPrefix(body.Link, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "link must be an in-app path"})
		return
	}
	if body.Audience == "" {
		body.Audience = "all"
	}
	if body.Audience != "all" && body.Audience != "active" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "audience must be all or active"})
		return
	}

	ctx := r.Context()
	where := `status <> 'banned'`
	if body.Audience == "active" {
		where = `status = 'active' AND (access_expires_at IS NULL OR access_expires_at > NOW())`
	}

	rows, err := s.Pool.Query(ctx, `SELECT id FROM users WHERE `+where)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	var recipients []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			recipients = append(recipients, id)
		}
	}
	rows.Close()

	ctxInsert, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	created := 0
	for _, uid := range recipients {
		if _, err := s.insertNotification(ctxInsert, uid, body.Category, body.Title, body.Body, body.Link); err == nil {
			created++
		}
	}

	var subscriptions int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM push_subscriptions`).Scan(&subscriptions)

	go s.dispatchPendingPush(recipients)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"recipients":    len(recipients),
		"inbox_created": created,
		"push_users":    subscriptions,
		"category":      body.Category,
		"quiet_hours":   time.Now().In(eatLocation).Hour() >= s.Cfg.QuietStartHour || time.Now().In(eatLocation).Hour() < s.Cfg.QuietEndHour,
		"push_enabled":  s.Cfg.PushEnabled(),
	})
}

func (s *Server) handleAdminListNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT title, body, COALESCE(link, ''), category,
		       to_char(date_trunc('minute', created_at) AT TIME ZONE 'Africa/Dar_es_Salaam', 'YYYY-MM-DD HH24:MI'),
		       COUNT(*)
		FROM notifications
		GROUP BY title, body, link, category, date_trunc('minute', created_at)
		ORDER BY date_trunc('minute', created_at) DESC
		LIMIT 30
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	defer rows.Close()

	type row struct {
		Title     string `json:"title"`
		Body      string `json:"body"`
		Link      string `json:"link"`
		Category  string `json:"category"`
		CreatedAt string `json:"created_at"`
		Count     int    `json:"count"`
	}
	out := []row{}
	for rows.Next() {
		var n row
		if err := rows.Scan(&n.Title, &n.Body, &n.Link, &n.Category, &n.CreatedAt, &n.Count); err == nil {
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"broadcasts": out})
}
