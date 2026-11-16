package config

import (
	"os"
	"strings"
)

// BackupConfig is the environment for cmd/backup, the scheduled off-site
// Postgres dump job. It is separate from the API config so the API image never
// needs object-storage credentials.
type BackupConfig struct {
	DatabaseURL string

	// S3-compatible destination (Railway bucket, Cloudflare R2, AWS S3).
	Bucket   string
	Endpoint string
	Region   string
	Prefix   string

	// Grandfather-father-son retention, in days.
	DailyKeepDays   int
	WeeklyKeepDays  int
	MonthlyKeepDays int

	// Binaries. Defaults assume the postgres image, which ships pg_dump 18.
	PgDump    string
	PgRestore string
}

// LoadBackup reads the backup job environment. Missing required values are
// reported by the caller, not silently defaulted.
func LoadBackup() *BackupConfig {
	return &BackupConfig{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		Bucket:          os.Getenv("BACKUP_S3_BUCKET"),
		Endpoint:        os.Getenv("BACKUP_S3_ENDPOINT"),
		Region:          getEnv("BACKUP_S3_REGION", "auto"),
		Prefix:          strings.Trim(getEnv("BACKUP_PREFIX", "db"), "/"),
		DailyKeepDays:   Atoi(getEnv("BACKUP_DAILY_KEEP_DAYS", "14"), 14),
		WeeklyKeepDays:  Atoi(getEnv("BACKUP_WEEKLY_KEEP_DAYS", "56"), 56),
		MonthlyKeepDays: Atoi(getEnv("BACKUP_MONTHLY_KEEP_DAYS", "183"), 183),
		PgDump:          getEnv("PG_DUMP", "pg_dump"),
		PgRestore:       getEnv("PG_RESTORE", "pg_restore"),
	}
}

// Missing returns the names of required settings that are empty.
func (c *BackupConfig) Missing() []string {
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"DATABASE_URL", c.DatabaseURL},
		{"BACKUP_S3_BUCKET", c.Bucket},
		{"AWS_ACCESS_KEY_ID", os.Getenv("AWS_ACCESS_KEY_ID")},
		{"AWS_SECRET_ACCESS_KEY", os.Getenv("AWS_SECRET_ACCESS_KEY")},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	return missing
}
