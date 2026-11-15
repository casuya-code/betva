package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	AdminUser   string
	AdminPass   string
	Env         string
	CORSOrigins []string
}

func Load() *Config {
	origins := []string{
		"http://localhost:4321",
		"http://127.0.0.1:4321",
		"http://localhost:3000",
	}
	if raw := os.Getenv("CORS_ORIGINS"); raw != "" {
		origins = nil
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}
	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://betva:betva@localhost:5432/betva?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", "change-me-in-production"),
		AdminUser:   getEnv("ADMIN_USERNAME", ""),
		AdminPass:   getEnv("ADMIN_PASSWORD", ""),
		Env:         getEnv("ENV", "development"),
		CORSOrigins: origins,
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
