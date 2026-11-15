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

		// protected
		api.Group(func(protected chi.Router) {
			protected.Use(s.authMiddleware)
			protected.Get("/me", s.handleMe)
			protected.Put("/me", s.handleUpdateMe)
			protected.Get("/history", s.handleHistory)
			protected.Post("/bets", s.handlePlaceBet)
		})

		// admin
		api.Route("/admin", func(adm chi.Router) {
			adm.Use(s.adminMiddleware)
			adm.Post("/matches/upload", s.handleUploadMatches)
			adm.Get("/users", s.handleAdminListUsers)
			adm.Put("/users/{id}/status", s.handleAdminUpdateUserStatus)
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
