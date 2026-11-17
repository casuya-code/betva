-- Betva migration 010: retryable push delivery.
--
-- An inbox row is permanent, but a push used to be dropped permanently on the
-- first failure (including quiet hours and the daily cap). These columns let
-- the dispatcher defer a push and try again later without sending it twice.

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS push_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS push_next_attempt_at TIMESTAMPTZ;

-- Partial index for the retry scheduler: only rows still waiting for a push.
CREATE INDEX IF NOT EXISTS idx_notifications_push_retry
    ON notifications (push_next_attempt_at, created_at)
    WHERE push_sent_at IS NULL;

-- A notification must never be counted twice against the daily cap, even if
-- two dispatchers race (the advisory lock is a second line of defence).
-- Old races may have left duplicates; keep one row per notification first so
-- the unique index can be created on live data.
DELETE FROM push_delivery_log a
USING push_delivery_log b
WHERE a.notification_id = b.notification_id
  AND a.notification_id IS NOT NULL
  AND a.ctid < b.ctid;

CREATE UNIQUE INDEX IF NOT EXISTS idx_push_delivery_notification_once
    ON push_delivery_log (notification_id)
    WHERE notification_id IS NOT NULL;
