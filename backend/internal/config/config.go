package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	AdminUser   string
	AdminPass   string
	Env         string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://betva:betva@localhost:5432/betva?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", "change-me-in-production"),
		AdminUser:   getEnv("ADMIN_USERNAME", ""),
		AdminPass:   getEnv("ADMIN_PASSWORD", ""),
		Env:         getEnv("ENV", "development"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c *Config) IsProd() bool { return c.Env == "production" }

func Atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
