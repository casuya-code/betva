-- Betva migration 015: small key/value store for admin-editable site settings.
--
-- Today this only holds the YouTube channel URL used by the subscribe button
-- on admin-published YouTube posts. Keeping it in the database (rather than a
-- build-time env var) means the admin can change it from the admin panel
-- without a Vercel redeploy, at zero streaming cost.
CREATE TABLE IF NOT EXISTS site_settings (
    key        VARCHAR(64) PRIMARY KEY,
    value      TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the row so reads always find a value (empty until an admin sets it).
INSERT INTO site_settings (key, value) VALUES ('youtube_channel_url', '')
    ON CONFLICT (key) DO NOTHING;
