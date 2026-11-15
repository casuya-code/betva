-- Betva migration 003: wallet, bets, settlement

-- Snapshot of the confidence shown when the bet was placed. Confidence may be
-- changed later, but the bet keeps what the user actually saw.
ALTER TABLE bets ADD COLUMN IF NOT EXISTS confidence_at_placement VARCHAR(10);
ALTER TABLE bets ADD COLUMN IF NOT EXISTS payout_multiplier_bp INT;
ALTER TABLE bets ADD COLUMN IF NOT EXISTS result_id UUID;
ALTER TABLE bets ADD COLUMN IF NOT EXISTS void_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_bets_user_placed ON bets(user_id, placed_at DESC);
CREATE INDEX IF NOT EXISTS idx_bets_pending ON bets(match_id) WHERE settled_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_ref ON ledger(ref_id);

-- Settlement marker on the match itself.
ALTER TABLE matches ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;

-- Results archive: corrections are new rows, never edits.
ALTER TABLE results_archive ADD COLUMN IF NOT EXISTS is_correction BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE results_archive ADD COLUMN IF NOT EXISTS corrects_id UUID;
ALTER TABLE results_archive ADD COLUMN IF NOT EXISTS reason TEXT;
ALTER TABLE results_archive ADD COLUMN IF NOT EXISTS reviewer VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_results_match ON results_archive(match_id, created_at DESC);

-- Bets may only use the two approved markets and the four approved states.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'bets_market_check') THEN
        ALTER TABLE bets ADD CONSTRAINT bets_market_check
            CHECK (market IN ('ft_over_0_5', 'ht_over_0_5'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'bets_status_check') THEN
        ALTER TABLE bets ADD CONSTRAINT bets_status_check
            CHECK (status IN ('pending', 'win', 'loss', 'refunded'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'bets_stake_positive') THEN
        ALTER TABLE bets ADD CONSTRAINT bets_stake_positive CHECK (stake_cents > 0);
    END IF;
END $$;

-- One bet per user, per match, per market. Keeps slips simple and prevents
-- accidental double-submits from a slow phone.
CREATE UNIQUE INDEX IF NOT EXISTS idx_bets_one_per_market
    ON bets(user_id, match_id, market);
