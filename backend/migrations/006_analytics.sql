-- Betva migration 006: daily active users (analytics)
--
-- One row per user per EAT day, appended on the user's first authenticated
-- request of that day. Append-only: it is a business record used for the
-- access-fee decision, so rows are never edited or deleted.

CREATE TABLE IF NOT EXISTS activity_days (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day        DATE        NOT NULL, -- EAT day (Africa/Dar_es_Salaam)
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, day)
);

CREATE INDEX IF NOT EXISTS idx_activity_days_day ON activity_days(day);
