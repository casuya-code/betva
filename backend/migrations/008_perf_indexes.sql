-- Performance indexes for hot read paths.
CREATE INDEX IF NOT EXISTS idx_post_reactions_post_reaction
    ON post_reactions (post_id, reaction);

CREATE INDEX IF NOT EXISTS idx_notifications_user_unread
    ON notifications (user_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_matches_date_status
    ON matches (match_date, status);