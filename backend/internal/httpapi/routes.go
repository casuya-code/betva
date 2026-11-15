package httpapi

import (
	"net/http"

	"github.com/betva/backend/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	Cfg  *config.Config
	Pool *pgxpool.Pool
	RDB  *redis.Client
}

func Register(r *chi.Mux, cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) {
	s := &Server{Cfg: cfg, Pool: pool, RDB: rdb}

	r.Get("/health", s.handleHealth)

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/ping", s.handlePing)

		// auth
		api.Post("/auth/register", s.handleRegister)
		api.Post("/auth/login", s.handleLogin)
		api.Post("/auth/logout", s.handleLogout)

		// matches (public read)
		api.Get("/matches", s.handleListMatches)
		api.Get("/config", s.handleConfig)

		// admin login uses env credentials, never the database
		api.Post("/admin/login", s.handleAdminLogin)

		// protected
		api.Group(func(protected chi.Router) {
			protected.Use(s.authMiddleware)
			protected.Get("/me", s.handleMe)
			protected.Put("/me", s.handleUpdateMe)
			protected.Get("/wallet", s.handleWallet)
			protected.Get("/history", s.handleHistory)
			protected.Post("/bets", s.handlePlaceBet)
		})

		// admin
		api.Route("/admin", func(adm chi.Router) {
			adm.Use(s.adminMiddleware)
			adm.Get("/me", s.handleAdminMe)

			adm.Get("/matches", s.handleAdminListMatches)
			adm.Post("/matches/upload", s.handleUploadMatches)
			adm.Put("/matches/{id}", s.handleAdminUpdateMatch)
			adm.Get("/matches/{id}/bets", s.handleAdminMatchBets)
			adm.Post("/matches/{id}/settle", s.handleAdminSettleMatch)
			adm.Post("/matches/{id}/refund", s.handleAdminRefundMatch)
			adm.Get("/imports", s.handleImportLog)

			adm.Get("/users", s.handleAdminListUsers)
			adm.Put("/users/{id}/status", s.handleAdminUpdateUserStatus)
			adm.Post("/users/{id}/credit", s.handleAdminCredit)
		})
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"betva-api"}`))
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"pong":true}`))
}
