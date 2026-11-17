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
	// Admin 2FA. When AdminTOTP is set, admin login also requires a current
	// six-digit TOTP code or the one-time recovery code. Both come from the
	// environment, never the database.
	AdminTOTP     string
	AdminRecovery string
	// PaymentNumber is the single official number users pay before the admin
	// activates their account. Empty means "ask the admin" in the UI.
	PaymentNumber string
	PaymentName   string
	Env           string
	CORSOrigins   []string

	// Wallet / betting rules.
	StakeTiers       []int64 // allowed stake amounts, in cents
	PayoutMultiplier int     // basis points, e.g. 15000 = 1.50x returned on a win
	MaxBetPerUserDay int64   // optional daily cap in cents; 0 = no cap

	// Community media.
	MediaDir      string
	MaxImageBytes int64
	MaxVideoBytes int64

	// Optional Cloudflare R2 storage for media. When all are set, uploads go
	// to R2 and the returned URL points at R2; otherwise MEDIA_DIR on disk.
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2MediaBucket     string
	R2MediaPublicBase string

	// Automatic match settlement. Results are only settled once a score is
	// available: either stored on the match by an admin or taken from the
	// newest admin-approved AI suggestion. A match with no result stays in the
	// honest 'awaiting_result' state until a human supplies one.
	AutoSettleEnabled         bool
	AutoSettleGraceMinutes    int // minutes after kickoff before a match is a candidate
	AutoSettleIntervalMinutes int // how often the sweep runs

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

	c := &Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://betva:betva@localhost:5432/betva?sslmode=disable"),
		RedisURL:      getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:     getEnv("JWT_SECRET", "change-me-in-production"),
		AdminUser:     getEnv("ADMIN_USERNAME", ""),
		AdminPass:     getEnv("ADMIN_PASSWORD", ""),
		AdminTOTP:     getEnv("ADMIN_TOTP_SECRET", ""),
		AdminRecovery: getEnv("ADMIN_RECOVERY_CODE", ""),
		PaymentNumber: getEnv("PAYMENT_NUMBER", ""),
		PaymentName:   getEnv("PAYMENT_NAME", ""),
		Env:           getEnv("ENV", "development"),
		CORSOrigins:   origins,

		StakeTiers:       parseStakeTiers(getEnv("STAKE_TIERS_CENTS", "50000,100000,200000,500000")),
		PayoutMultiplier: Atoi(getEnv("PAYOUT_MULTIPLIER_BP", "15000"), 15000),
		MaxBetPerUserDay: int64(Atoi(getEnv("MAX_BET_PER_DAY_CENTS", "0"), 0)),

		MediaDir:      getEnv("MEDIA_DIR", "media"),
		MaxImageBytes: int64(Atoi(getEnv("MAX_IMAGE_BYTES", "400000"), 400000)),
		MaxVideoBytes: int64(Atoi(getEnv("MAX_VIDEO_BYTES", "3000000"), 3000000)),

		R2AccountID:       getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2MediaBucket:     os.Getenv("R2_MEDIA_BUCKET"),
		R2MediaPublicBase: getEnv("R2_MEDIA_PUBLIC_URL", ""),

		AutoSettleEnabled:         getEnv("AUTO_SETTLE_ENABLED", "1") != "0",
		AutoSettleGraceMinutes:    Atoi(getEnv("AUTO_SETTLE_GRACE_MINUTES", "120"), 120),
		AutoSettleIntervalMinutes: Atoi(getEnv("AUTO_SETTLE_INTERVAL_MINUTES", "15"), 15),

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

	// A zero/negative grace means "settle as soon as kickoff passes"; a bad
	// interval is clamped so a typo cannot make the sweep hammer the database.
	if c.AutoSettleGraceMinutes < 0 {
		c.AutoSettleGraceMinutes = 0
	}
	if c.AutoSettleIntervalMinutes < 1 {
		c.AutoSettleIntervalMinutes = 15
	}

	// Bad push settings must not take the whole app down. Clamp them to sane
	// values so quiet-hour and cap checks can never produce an impossible window.
	if c.PushDailyCap < 0 {
		c.PushDailyCap = 0
	}
	c.QuietStartHour, _ = clampHour(c.QuietStartHour, 22)
	c.QuietEndHour, _ = clampHour(c.QuietEndHour, 7)
	if c.QuietStartHour == c.QuietEndHour {
		c.QuietEndHour = (c.QuietEndHour + 1) % 24
	}

	return c
}

func clampHour(hour, fallback int) (int, bool) {
	if hour < 0 || hour > 23 {
		return fallback, false
	}
	return hour, true
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
