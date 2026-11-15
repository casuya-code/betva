-- Betva migration 002: matches admin, import log, user status history

-- Prevent duplicate fixtures on the same date.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'matches_unique_fixture'
    ) THEN
        ALTER TABLE matches
            ADD CONSTRAINT matches_unique_fixture
            UNIQUE (match_date, home_team, away_team);
    END IF;
END $$;

-- Every CSV upload is recorded here.
CREATE TABLE IF NOT EXISTS import_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_name    VARCHAR(64) NOT NULL DEFAULT 'admin',
    match_date    DATE        NOT NULL,
    file_name     VARCHAR(255),
    rows_total    INT         NOT NULL DEFAULT 0,
    rows_imported INT         NOT NULL DEFAULT 0,
    rows_failed   INT         NOT NULL DEFAULT 0,
    replaced      BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_import_log_date ON import_log(match_date, created_at DESC);

-- Access contribution expiry (manual activation by admin).
ALTER TABLE users ADD COLUMN IF NOT EXISTS access_expires_at TIMESTAMPTZ;

-- Audit trail for every status change (ban, activation, suspension).
CREATE TABLE IF NOT EXISTS user_status_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    old_status  VARCHAR(20) NOT NULL,
    new_status  VARCHAR(20) NOT NULL,
    reason      TEXT,
    reviewer    VARCHAR(64) NOT NULL DEFAULT 'admin',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_status_log_user ON user_status_log(user_id, created_at DESC);
