package db

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.ParseConfig: %w", err)
	}
	// Default to a larger pool than pgx's 4 so concurrent requests (bets,
	// feed, admin) don't queue behind a tiny connection count.
	maxConns := int32(20)
	if raw := os.Getenv("DB_MAX_CONNS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxConns = int32(n)
		}
	}
	cfg.MaxConns = maxConns
	if cfg.MinConns == 0 {
		cfg.MinConns = 2
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return pool, nil
}