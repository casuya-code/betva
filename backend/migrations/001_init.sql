-- Betva initial schema

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nickname      VARCHAR(64)  NOT NULL,
    phone         VARCHAR(20)  NOT NULL UNIQUE,
    official_name VARCHAR(128) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    profile_pic   TEXT,
    status        VARCHAR(20)  NOT NULL DEFAULT 'inactive', -- inactive, active, banned, suspended
    balance_cents BIGINT       NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_phone ON users(phone);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);

CREATE TABLE IF NOT EXISTS consent_log (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notice_version INT  NOT NULL,
    accepted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS matches (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_date    DATE         NOT NULL,
    kickoff_time  TIME         NOT NULL,
    league        VARCHAR(128),
    home_team     VARCHAR(128) NOT NULL,
    away_team     VARCHAR(128) NOT NULL,
    status        VARCHAR(20)  NOT NULL DEFAULT 'upcoming', -- upcoming, live, finished, postponed, cancelled
    confidence    VARCHAR(10)  NOT NULL DEFAULT 'medium',   -- low, medium, high
    home_goals    INT,
    away_goals    INT,
    home_goals_ht INT,
    away_goals_ht INT,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_matches_date ON matches(match_date);

CREATE TABLE IF NOT EXISTS match_markets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id   UUID NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
    market     VARCHAR(40) NOT NULL, -- full_time_over_0_5, over_0_5_ht
    UNIQUE (match_id, market)
);

CREATE TABLE IF NOT EXISTS bets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    match_id    UUID NOT NULL REFERENCES matches(id),
    market      VARCHAR(40) NOT NULL,
    stake_cents BIGINT NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, win, loss, refunded
    payout_cents BIGINT NOT NULL DEFAULT 0,
    placed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bets_user ON bets(user_id);
CREATE INDEX IF NOT EXISTS idx_bets_match ON bets(match_id);

CREATE TABLE IF NOT EXISTS ledger (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    amount_cents BIGINT NOT NULL, -- positive credit, negative debit
    reason      VARCHAR(64) NOT NULL,
    ref_id      UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_user ON ledger(user_id);

CREATE TABLE IF NOT EXISTS results_archive (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id    UUID NOT NULL REFERENCES matches(id),
    result_date DATE NOT NULL,
    home_goals  INT  NOT NULL,
    away_goals  INT  NOT NULL,
    home_goals_ht INT,
    away_goals_ht INT,
    source      VARCHAR(64),
    reviewer_id UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_results_date ON results_archive(result_date);
