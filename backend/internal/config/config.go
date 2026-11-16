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

	// Wallet / betting rules.
	StakeTiers       []int64 // allowed stake amounts, in cents
	PayoutMultiplier int     // basis points, e.g. 15000 = 1.50x returned on a win
	MaxBetPerUserDay int64   // optional daily cap in cents; 0 = no cap

	// Community media.
	MediaDir      string
	MaxImageBytes int64
	MaxVideoBytes int64

	// Push notifications.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	PushDailyCap    int // max pushes per user per EAT day
	QuietStartHour  int // 22 = 22:00 EAT
	QuietEndHour    int // 7 = 07:00 EAT

	// AI results assistant (Groq, inference-only).
	GroqAPIKey string
	GroqModel  string

	// Access-fee planner (admin analytics). The admin sets the DAU target and
	// the monthly running cost here; the analytics endpoint turns them into a
	// suggested per-user monthly contribution. These never reach users.
	FeeTargetDAU     int   // ACCESS_FEE_DAU_TARGET, 0 = not configured
	MonthlyCostCents int64 // MONTHLY_OPERATING_COST_CENTS, 0 = not configured

	// AccessFeeCents is the flat rate users actually pay per month for access.
	// This IS user-visible (Profile / config endpoint).
	AccessFeeCents int64 // ACCESS_FEE_CENTS, 0 = not set
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

		StakeTiers:       parseStakeTiers(getEnv("STAKE_TIERS_CENTS", "50000,100000,200000,500000")),
		PayoutMultiplier: Atoi(getEnv("PAYOUT_MULTIPLIER_BP", "15000"), 15000),
		MaxBetPerUserDay: int64(Atoi(getEnv("MAX_BET_PER_DAY_CENTS", "0"), 0)),

		MediaDir:      getEnv("MEDIA_DIR", "media"),
		MaxImageBytes: int64(Atoi(getEnv("MAX_IMAGE_BYTES", "400000"), 400000)),
		MaxVideoBytes: int64(Atoi(getEnv("MAX_VIDEO_BYTES", "3000000"), 3000000)),

		VAPIDPublicKey:  getEnv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey: getEnv("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:    getEnv("VAPID_SUBJECT", "mailto:admin@betva.local"),
		PushDailyCap:    Atoi(getEnv("PUSH_DAILY_CAP", "2"), 2),
		QuietStartHour:  Atoi(getEnv("QUIET_START_HOUR", "22"), 22),
		QuietEndHour:    Atoi(getEnv("QUIET_END_HOUR", "7"), 7),

		GroqAPIKey: getEnv("GROQ_API_KEY", ""),
		GroqModel:  getEnv("GROQ_MODEL", "openai/gpt-oss-20b"),

		FeeTargetDAU:     Atoi(getEnv("ACCESS_FEE_DAU_TARGET", "0"), 0),
		MonthlyCostCents: int64(Atoi(getEnv("MONTHLY_OPERATING_COST_CENTS", "0"), 0)),
		AccessFeeCents:   int64(Atoi(getEnv("ACCESS_FEE_CENTS", "0"), 0)),
	}
}

// PushEnabled reports whether VAPID keys are configured.
func (c *Config) PushEnabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
}

func parseStakeTiers(raw string) []int64 {
	var out []int64
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err == nil && n > 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		out = []int64{50000, 100000, 200000, 500000}
	}
	return out
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c *Config) IsProd() bool { return c.Env == "production" }

// IsValidStake reports whether cents is one of the fixed stake tiers.
func (c *Config) IsValidStake(cents int64) bool {
	for _, t := range c.StakeTiers {
		if t == cents {
			return true
		}
	}
	return false
}

// PayoutFor returns the gross amount credited on a winning bet.
// The stake was already debited at placement, so this is the full return.
func (c *Config) PayoutFor(stakeCents int64) int64 {
	return stakeCents * int64(c.PayoutMultiplier) / 10000
}

func Atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
