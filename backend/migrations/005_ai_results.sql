-- Betva migration 005: AI result suggestions (Groq, inference-only)
--
-- An AI suggestion is NEVER a settlement. It is a proposed score that an admin
-- must review and approve before the normal settle endpoint is called. The
-- original result and any payout are written by the settle flow, not here.

CREATE TABLE IF NOT EXISTS result_suggestions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id           UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    source_url         TEXT NOT NULL,
    source_excerpt     TEXT NOT NULL DEFAULT '',
    suggested_home     INT,
    suggested_away     INT,
    suggested_home_ht  INT,
    suggested_away_ht  INT,
    confidence         VARCHAR(8),
    provider           VARCHAR(16) NOT NULL DEFAULT 'groq',
    model              VARCHAR(64) NOT NULL DEFAULT '',
    raw_response       JSONB,
    status             VARCHAR(12) NOT NULL DEFAULT 'pending',
    reviewed_by        VARCHAR(64),
    reviewed_at        TIMESTAMPTZ,
    review_reason      TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT result_suggestions_status_check
        CHECK (status IN ('pending', 'approved', 'rejected', 'failed')),
    CONSTRAINT result_suggestions_confidence_check
        CHECK (confidence IS NULL OR confidence IN ('high', 'medium', 'low')),
    CONSTRAINT result_suggestions_goals_check
        CHECK (suggested_home IS NULL OR (suggested_home >= 0 AND suggested_home <= 30)),
    CONSTRAINT result_suggestions_goals_away_check
        CHECK (suggested_away IS NULL OR (suggested_away >= 0 AND suggested_away <= 30))
);

CREATE INDEX IF NOT EXISTS idx_result_suggestions_match ON result_suggestions(match_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_result_suggestions_status ON result_suggestions(status, created_at DESC);
