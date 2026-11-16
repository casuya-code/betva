-- Betva migration 009: per-market odds + audited deletion of wrong uploads

-- Odds are stored in basis points on each offered market (15000 = 1.50x).
-- NULL means the upload carried no odds: fall back to PAYOUT_MULTIPLIER_BP.
ALTER TABLE match_markets
    ADD COLUMN IF NOT EXISTS odds_bp INT CHECK (odds_bp IS NULL OR odds_bp > 0);

-- A wrongly uploaded match is removed, but the reason and the operator are
-- kept. No FK on match_id: the audit row survives the deletion it describes.
CREATE TABLE IF NOT EXISTS match_delete_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id     UUID        NOT NULL,
    match_date   DATE        NOT NULL,
    kickoff_time TIME        NOT NULL,
    home_team    VARCHAR(128) NOT NULL,
    away_team    VARCHAR(128) NOT NULL,
    reason       TEXT,
    reviewer     VARCHAR(64) NOT NULL DEFAULT 'admin',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_match_delete_log_created ON match_delete_log(created_at DESC);
