-- Betva migration 004: community feed, notifications, push, self-exclusion

-- ---------------------------------------------------------------------------
-- Community
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS posts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body           TEXT NOT NULL DEFAULT '',
    media_url      TEXT,
    media_type     VARCHAR(16), -- image | video
    media_bytes    BIGINT NOT NULL DEFAULT 0,
    status         VARCHAR(16) NOT NULL DEFAULT 'visible', -- visible | hidden | deleted
    moderation_reason TEXT,
    moderated_by   VARCHAR(64),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT posts_not_empty CHECK (length(trim(body)) > 0 OR media_url IS NOT NULL),
    CONSTRAINT posts_status_check CHECK (status IN ('visible', 'hidden', 'deleted')),
    CONSTRAINT posts_media_type_check CHECK (media_type IS NULL OR media_type IN ('image', 'video')),
    CONSTRAINT posts_body_len CHECK (length(body) <= 500)
);

CREATE INDEX IF NOT EXISTS idx_posts_feed ON posts(created_at DESC) WHERE status = 'visible';
CREATE INDEX IF NOT EXISTS idx_posts_user ON posts(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS post_reports (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolution  VARCHAR(32)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_post_reports_once ON post_reports(post_id, reporter_id);
CREATE INDEX IF NOT EXISTS idx_post_reports_open ON post_reports(created_at DESC) WHERE resolved_at IS NULL;

-- ---------------------------------------------------------------------------
-- Notifications
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_prefs (
    user_id       UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    match_start   BOOLEAN NOT NULL DEFAULT TRUE,
    results       BOOLEAN NOT NULL DEFAULT TRUE,
    account       BOOLEAN NOT NULL DEFAULT TRUE,
    announcements BOOLEAN NOT NULL DEFAULT TRUE,
    community     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category     VARCHAR(24) NOT NULL,
    title        VARCHAR(120) NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    link         VARCHAR(255),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at      TIMESTAMPTZ,
    push_sent_at TIMESTAMPTZ,
    push_error   TEXT,
    CONSTRAINT notifications_category_check
        CHECK (category IN ('match_start', 'results', 'account', 'announcements', 'community'))
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint     TEXT NOT NULL UNIQUE,
    p256dh       TEXT NOT NULL,
    auth         TEXT NOT NULL,
    user_agent   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions(user_id);

-- One row per push actually handed to a push service. Used to enforce the
-- 2-per-day cap without counting inbox rows.
CREATE TABLE IF NOT EXISTS push_delivery_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    notification_id UUID REFERENCES notifications(id) ON DELETE CASCADE,
    sent_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_push_delivery_user_day ON push_delivery_log(user_id, sent_at DESC);

-- ---------------------------------------------------------------------------
-- Self-exclusion (append-only; a user cannot shorten or undo a period)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS self_exclusions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    until          TIMESTAMPTZ NOT NULL,
    duration_hours INT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT self_exclusions_duration_check CHECK (duration_hours IN (24, 168, 720))
);

CREATE INDEX IF NOT EXISTS idx_self_exclusions_user ON self_exclusions(user_id, until DESC);
