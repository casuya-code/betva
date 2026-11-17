package httpapi

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/betva/backend/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	Cfg   *config.Config
	Pool  *pgxpool.Pool
	RDB   *redis.Client
	Media *mediaStore

	// Short-lived JSON caches for public, aggregate data. Redis-backed when
	// available so the cache is shared across replicas, in-memory otherwise.
	// See cache.go.
	statsCache *jsonCache

	// Rate limiters. Redis-backed when available so limits are shared across
	// replicas, in-memory per-process otherwise; see ratelimit.go.
	loginIP       *RateLimiter
	loginPhone    *RateLimiter
	registerIP    *RateLimiter
	registerPhone *RateLimiter
	adminLoginIP  *RateLimiter
	mediaUpload   *RateLimiter
	aiSuggest     *RateLimiter

	// Daily-active-user tracking: per-day in-memory seen set, backed by the
	// append-only activity_days table.
	actMu   sync.Mutex
	actDay  string
	actSeen map[string]bool
}

func Register(r *chi.Mux, cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) {
	s := &Server{
		Cfg:           cfg,
		Pool:          pool,
		RDB:           rdb,
		Media:         newMediaStore(cfg),
		statsCache:    newJSONCache("betva:stats:v1", 60*time.Second, rdb),
		loginIP:       NewSharedRateLimiter(60, 5*time.Minute, rdb, "login_ip"),
		loginPhone:    NewSharedRateLimiter(10, 15*time.Minute, rdb, "login_phone"),
		registerIP:    NewSharedRateLimiter(30, time.Hour, rdb, "register_ip"),
		registerPhone: NewSharedRateLimiter(3, time.Hour, rdb, "register_phone"),
		adminLoginIP:  NewSharedRateLimiter(20, 5*time.Minute, rdb, "admin_login_ip"),
		mediaUpload:   NewSharedRateLimiter(30, time.Hour, rdb, "media_upload"),
		aiSuggest:     NewSharedRateLimiter(30, time.Hour, rdb, "ai_suggest"),
	}

	// Broad flood guard. Auth, upload, and AI routes add stricter limits.
	r.Use(NewSharedRateLimiter(600, time.Minute, rdb, "global").Middleware)

	if err := os.MkdirAll(cfg.MediaDir, 0o755); err != nil {
		log.Printf("media dir: %v", err)
	}
	r.Handle("/media/*", http.StripPrefix("/media/", mediaCache(http.FileServer(http.Dir(cfg.MediaDir)))))

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/ping", s.handlePing)

		// auth
		api.Post("/auth/register", s.registerIP.Wrap(s.handleRegister))
		api.Post("/auth/login", s.loginIP.Wrap(s.handleLogin))
		api.Post("/auth/logout", s.handleLogout)

		// matches (public read)
		api.Get("/matches", s.handleListMatches)
		api.Get("/matches/history", s.handleMatchHistory)
		api.Get("/config", s.handleConfig)
		api.Get("/stats", s.handleStats)

		// admin login uses env credentials, never the database
		api.Post("/admin/login", s.adminLoginIP.Wrap(s.handleAdminLogin))
		api.Get("/admin/2fa/status", s.handleAdmin2FAStatus)

		// protected
		api.Group(func(protected chi.Router) {
			protected.Use(s.authMiddleware)
			protected.Get("/me", s.handleMe)
			protected.Put("/me", s.handleUpdateMe)
			protected.Get("/wallet", s.handleWallet)
			protected.Get("/history", s.handleHistory)
			protected.Post("/bets", s.handlePlaceBet)

			protected.Get("/community/posts", s.handleCommunityFeed)
			protected.Post("/community/posts", s.handleCreatePost)
			protected.Post("/community/posts/{id}/react", s.handleReactPost)
			protected.Delete("/community/posts/{id}", s.handleDeleteOwnPost)
			protected.Post("/community/posts/{id}/report", s.handleReportPost)
			protected.Post("/media", s.mediaUpload.WrapUser(s.handleMediaUpload))

			protected.Get("/notifications", s.handleListNotifications)
			protected.Post("/notifications/{id}/read", s.handleMarkNotificationRead)
			protected.Post("/notifications/read-all", s.handleMarkAllNotificationsRead)
			protected.Get("/notifications/prefs", s.handleGetNotificationPrefs)
			protected.Put("/notifications/prefs", s.handleUpdateNotificationPrefs)
			protected.Post("/push/subscribe", s.handleSubscribePush)
			protected.Post("/push/unsubscribe", s.handleUnsubscribePush)

			protected.Get("/self-exclusion", s.handleGetSelfExclusion)
			protected.Post("/self-exclusion", s.handleSetSelfExclusion)
		})

		StartSettlementScheduler(s)
		StartPushScheduler(s)

		// admin
		api.Route("/admin", func(adm chi.Router) {
			adm.Use(s.adminMiddleware)
			adm.Get("/me", s.handleAdminMe)

			adm.Get("/matches", s.handleAdminListMatches)
			adm.Post("/matches/upload", s.handleUploadMatches)
			adm.Put("/matches/{id}", s.handleAdminUpdateMatch)
			adm.Delete("/matches/{id}", s.handleAdminDeleteMatch)
			adm.Get("/matches/{id}/bets", s.handleAdminMatchBets)
			adm.Post("/matches/{id}/settle", s.handleAdminSettleMatch)
			adm.Post("/matches/{id}/refund", s.handleAdminRefundMatch)
			adm.Post("/results/scan", s.handleAdminScanResults)
			adm.Get("/imports", s.handleImportLog)

			adm.Get("/users", s.handleAdminListUsers)
			adm.Put("/users/{id}/status", s.handleAdminUpdateUserStatus)
			adm.Post("/users/{id}/credit", s.handleAdminCredit)
			adm.Get("/analytics", s.handleAdminAnalytics)

			adm.Get("/community/posts", s.handleAdminCommunityPosts)
			adm.Put("/community/posts/{id}/status", s.handleAdminModeratePost)
			adm.Post("/notifications/send", s.handleAdminSendNotification)
			adm.Get("/notifications", s.handleAdminListNotifications)

			adm.Post("/ai/suggest", s.aiSuggest.WrapUser(s.handleAdminGenerateSuggestion))
			adm.Get("/ai/suggestions", s.handleAdminListSuggestions)
			adm.Put("/ai/suggestions/{id}/review", s.handleAdminReviewSuggestion)
		})
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"betva-api"}`))
}

// mediaCache marks uploaded media as immutable for a year. Uploads are stored
// under content-hash filenames, so the same URL never changes content — phones
// on slow networks should never re-download an image they already have.
func mediaCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"pong":true}`))
}
