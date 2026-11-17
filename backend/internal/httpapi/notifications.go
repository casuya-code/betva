package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
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

	// Push delivery is bounded so one large broadcast cannot monopolise the
	// process. Anything left over is picked up by the retry scheduler.
	pushBatchSize      = 500
	pushMaxAttempts    = 5
	pushDispatchBudget = 50 * time.Second
	pushLockKey        = int64(4815162342)
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
			if _, err := s.insertNotification(ctx, uid, category, title, body, link); err != nil {
				log.Printf("push inbox insert for %s: %v", uid, err)
			}
		}
		s.dispatchPendingPush(ids)
	}(append([]string(nil), userIDs...))
}

type pushNotice struct {
	ID, UserID, Category, Title, Body, Link string
	Attempts                                int
}

// acquirePushLock makes sure only one dispatcher is active in the process at a
// time. The advisory lock lives on a dedicated pooled connection for exactly
// as long as the dispatch runs.
func (s *Server) acquirePushLock(ctx context.Context) (func(), bool) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		log.Printf("push lock acquire: %v", err)
		return nil, false
	}
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, pushLockKey).Scan(&got); err != nil || !got {
		if err != nil {
			log.Printf("push lock: %v", err)
		}
		conn.Release()
		return nil, false
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, pushLockKey)
		conn.Release()
	}, true
}

// quietNow supports both overnight (22→07) and daytime (09→17) windows.
func (s *Server) quietNow(now time.Time) bool {
	hour := now.Hour()
	if s.Cfg.QuietStartHour > s.Cfg.QuietEndHour {
		return hour >= s.Cfg.QuietStartHour || hour < s.Cfg.QuietEndHour
	}
	return hour >= s.Cfg.QuietStartHour && hour < s.Cfg.QuietEndHour
}

func (s *Server) pushDeferUntil(now time.Time, reason string) time.Time {
	switch reason {
	case "quiet hours":
		next := time.Date(now.Year(), now.Month(), now.Day(), s.Cfg.QuietEndHour, 1, 0, 0, eatLocation)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	case "daily cap reached":
		return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 1, 0, 0, eatLocation)
	case "muted by user":
		return now.Add(6 * time.Hour)
	case "no subscription", "push not configured":
		return now.Add(time.Hour)
	default:
		return time.Time{}
	}
}

func pushBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

func (s *Server) markPushDeferred(ctx context.Context, id, reason string, until time.Time) {
	var next interface{}
	if !until.IsZero() {
		next = until
	}
	if _, err := s.Pool.Exec(ctx, `
		UPDATE notifications SET push_error = $2, push_next_attempt_at = $3
		WHERE id = $1 AND push_sent_at IS NULL
	`, id, reason, next); err != nil {
		log.Printf("push defer %s: %v", id, err)
	}
}

// dispatchPendingPush drains the retry queue. A reason such as quiet hours or
// a daily cap is a deferral, not a failure: the row stays eligible until its
// next attempt time. Real transport failures back off, then stop after
// pushMaxAttempts so a dead endpoint cannot be retried forever.
func (s *Server) dispatchPendingPush(userIDs []string) {
	// A nil userID list means "everyone" (used by the retry scheduler). An
	// empty non-nil list means there is nothing to do.
	if userIDs != nil && len(userIDs) == 0 {
		return
	}
	if !s.Cfg.PushEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pushDispatchBudget)
	defer cancel()

	release, ok := s.acquirePushLock(ctx)
	if !ok {
		log.Printf("push dispatch: another dispatcher is active, skipping")
		return
	}
	defer release()

	total, deferred, failed := 0, 0, 0
	for {
		if ctx.Err() != nil {
			log.Printf("push dispatch: time budget reached, leaving the rest for retry")
			break
		}
		sent, def, fail, err := s.dispatchPushBatch(ctx, userIDs)
		if err != nil {
			log.Printf("push dispatch batch: %v", err)
			break
		}
		total += sent + def + fail
		deferred += def
		failed += fail
		if sent+def+fail == 0 {
			break
		}
	}
	if total > 0 {
		log.Printf("push dispatch: processed=%d deferred=%d failed=%d", total, deferred, failed)
	}
}

// dispatchPushBatch claims one LIMIT-sized slice of pending notifications.
// Rows are sent again on the next loop because sent/deferred rows no longer
// match the pending filter, so large broadcasts are not capped at 500.
func (s *Server) dispatchPushBatch(ctx context.Context, userIDs []string) (sent, deferred, failed int, err error) {
	query := `
		SELECT n.id, n.user_id, n.category, n.title, n.body, COALESCE(n.link, ''), n.push_attempts
		FROM notifications n
		WHERE n.push_sent_at IS NULL
		  AND n.push_attempts < $1
		  AND (n.push_next_attempt_at IS NULL OR n.push_next_attempt_at <= NOW())
	`
	args := []interface{}{pushMaxAttempts}
	if len(userIDs) > 0 {
		query += ` AND n.user_id = ANY($2::uuid[])`
		args = append(args, userIDs)
	}
	query += ` ORDER BY n.created_at ASC LIMIT ` + strconv.Itoa(pushBatchSize)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return 0, 0, 0, err
	}
	var pending []pushNotice
	for rows.Next() {
		var n pushNotice
		if err := rows.Scan(&n.ID, &n.UserID, &n.Category, &n.Title, &n.Body, &n.Link, &n.Attempts); err == nil {
			pending = append(pending, n)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, 0, err
	}
	rows.Close()

	for _, n := range pending {
		now := time.Now().In(eatLocation)

		if !s.Cfg.PushEnabled() {
			s.markPushDeferred(ctx, n.ID, "push not configured", s.pushDeferUntil(now, "push not configured"))
			deferred++
			continue
		}
		if s.quietNow(now) {
			s.markPushDeferred(ctx, n.ID, "quiet hours", s.pushDeferUntil(now, "quiet hours"))
			deferred++
			continue
		}

		var muted bool
		_ = s.Pool.QueryRow(ctx, `
			SELECT COALESCE((
				SELECT NOT `+prefsColumn(n.Category)+` FROM notification_prefs WHERE user_id = $1
			), FALSE)
		`, n.UserID).Scan(&muted)
		if muted {
			s.markPushDeferred(ctx, n.ID, "muted by user", s.pushDeferUntil(now, "muted by user"))
			deferred++
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
			s.markPushDeferred(ctx, n.ID, "daily cap reached", s.pushDeferUntil(now, "daily cap reached"))
			deferred++
			continue
		}

		subRows, err := s.Pool.Query(ctx, `
			SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = $1
		`, n.UserID)
		if err != nil {
			log.Printf("push subscriptions for %s: %v", n.UserID, err)
			failed++
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
		if err := subRows.Err(); err != nil {
			log.Printf("push subscriptions scan for %s: %v", n.UserID, err)
		}
		subRows.Close()
		if len(subs) == 0 {
			s.markPushDeferred(ctx, n.ID, "no subscription", s.pushDeferUntil(now, "no subscription"))
			deferred++
			continue
		}

		attempts := n.Attempts + 1
		if _, err := s.Pool.Exec(ctx, `
			UPDATE notifications SET push_attempts = $1 WHERE id = $2 AND push_sent_at IS NULL
		`, attempts, n.ID); err != nil {
			log.Printf("push attempt update %s: %v", n.ID, err)
			failed++
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
			resp, sendErr := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
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
			if sendErr != nil {
				lastErr = sendErr.Error()
				continue
			}
			status := resp.StatusCode
			_ = resp.Body.Close()
			// 403 means the keys no longer match this subscription (VAPID
			// rotation); 404/410 mean the browser has forgotten it.
			if status == http.StatusForbidden || status == http.StatusNotFound || status == http.StatusGone {
				if _, err := s.Pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, sub.ID); err != nil {
					log.Printf("push subscription prune %s: %v", sub.ID, err)
				}
			}
			if status >= 200 && status < 300 {
				delivered = true
			} else {
				lastErr = "push service returned " + strconv.Itoa(status)
			}
		}

		if delivered {
			tx, txErr := s.Pool.Begin(ctx)
			if txErr != nil {
				log.Printf("push tx begin %s: %v", n.ID, txErr)
				failed++
				continue
			}
			if _, txErr = tx.Exec(ctx, `
				UPDATE notifications
				SET push_sent_at = NOW(), push_error = NULL, push_next_attempt_at = NULL
				WHERE id = $1 AND push_sent_at IS NULL
			`, n.ID); txErr == nil {
				_, txErr = tx.Exec(ctx, `
					INSERT INTO push_delivery_log (user_id, notification_id)
					SELECT $1, $2 WHERE NOT EXISTS (
						SELECT 1 FROM push_delivery_log WHERE notification_id = $2
					)
				`, n.UserID, n.ID)
			}
			if txErr != nil {
				_ = tx.Rollback(ctx)
				log.Printf("push bookkeeping %s: %v", n.ID, txErr)
				failed++
				continue
			}
			if txErr = tx.Commit(ctx); txErr != nil {
				log.Printf("push commit %s: %v", n.ID, txErr)
				failed++
				continue
			}
			sent++
			continue
		}

		if lastErr == "" {
			lastErr = "push failed"
		}
		reason := lastErr
		var until time.Time
		if attempts < pushMaxAttempts {
			until = now.Add(pushBackoff(attempts))
		} else {
			reason = "giving up: " + lastErr
		}
		s.markPushDeferred(ctx, n.ID, reason, until)
		log.Printf("push %s attempt %d: %s", n.ID, attempts, lastErr)
		failed++
	}

	return sent, deferred, failed, nil
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

	// A user who unmutes a category should not wait for the next retry tick.
	_, _ = s.Pool.Exec(ctx, `
		UPDATE notifications SET push_error = NULL, push_next_attempt_at = NULL
		WHERE user_id = $1 AND push_sent_at IS NULL AND push_error = 'muted by user'
	`, u.ID)
	go s.dispatchPendingPush([]string{u.ID})

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
	body.P256DH = strings.TrimRight(body.P256DH, "=")
	body.Auth = strings.TrimRight(body.Auth, "=")
	if body.Endpoint == "" || body.P256DH == "" || body.Auth == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "endpoint, p256dh, and auth are required"})
		return
	}
	if len(body.Endpoint) > 2048 || !strings.HasPrefix(body.Endpoint, "https://") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid endpoint"})
		return
	}
	// P-256 public keys are 65 raw bytes; auth secrets are at least 16.
	// Rejecting junk here stops a malformed browser subscription from silently
	// failing on every later send.
	key, keyErr := base64.RawURLEncoding.DecodeString(body.P256DH)
	auth, authErr := base64.RawURLEncoding.DecodeString(body.Auth)
	if keyErr != nil || len(key) != 65 || authErr != nil || len(auth) < 16 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid subscription keys"})
		return
	}

	ctx := r.Context()
	var owned, sameEndpoint int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1`, u.ID).Scan(&owned)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2`, body.Endpoint, u.ID).Scan(&sameEndpoint)
	if owned >= 10 && sameEndpoint == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many devices subscribed"})
		return
	}

	_, err := s.Pool.Exec(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh,
		    auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent, last_seen_at = NOW()
	`, u.ID, body.Endpoint, body.P256DH, body.Auth, r.UserAgent())
	if err != nil {
		log.Printf("push subscribe %s: %v", u.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	// A new or refreshed device unblocks messages that were deferred because
	// this account had no usable subscription.
	_, _ = s.Pool.Exec(ctx, `
		UPDATE notifications SET push_error = NULL, push_next_attempt_at = NULL
		WHERE user_id = $1 AND push_sent_at IS NULL
		  AND push_error IN ('no subscription', 'push not configured', 'giving up: no subscription')
	`, u.ID)
	go s.dispatchPendingPush([]string{u.ID})

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
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2`, body.Endpoint, u.ID); err != nil {
		log.Printf("push unsubscribe %s: %v", u.ID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
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
	if body.Hours != 3 && body.Hours != 6 && body.Hours != 12 && body.Hours != 24 && body.Hours != 168 && body.Hours != 720 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hours must be 3, 6, 12, 24, 168, or 720"})
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
		// Log the real cause server-side. Returning only "db error" to the
		// client hid a constraint mismatch for a long time; keep the client
		// message generic but make the failure diagnosable in deploy logs.
		log.Printf("self-exclusion insert failed for user %s, hours=%d: %v", u.ID, body.Hours, err)
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
	if body.Link != "" && (!strings.HasPrefix(body.Link, "/") || strings.HasPrefix(body.Link, "//")) {
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

	// Report subscribed users inside the chosen audience, not a global count.
	var subscriptions int
	_ = s.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT ps.user_id)
		FROM push_subscriptions ps JOIN users u ON u.id = ps.user_id
		WHERE `+where).Scan(&subscriptions)

	go s.dispatchPendingPush(recipients)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"recipients":    len(recipients),
		"inbox_created": created,
		"push_users":    subscriptions,
		"category":      body.Category,
		"quiet_hours":   s.quietNow(time.Now().In(eatLocation)),
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
