-- Betva migration 014: admin-published YouTube posts.
--
-- R2/local uploads are unchanged. This adds a second, admin-only way to put a
-- video in the community feed: paste a YouTube URL and only the 11-character
-- video ID is stored. Playback happens in YouTube's iframe player on the
-- Vercel frontend, so the backend stores ~11 bytes per video and streams
-- nothing — every play counts as a YouTube view instead of Railway bandwidth.

-- Allow the new media type alongside image/video.
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_media_type_check;
ALTER TABLE posts ADD CONSTRAINT posts_media_type_check
    CHECK (media_type IS NULL OR media_type IN ('image', 'video', 'youtube'));

-- The 11-character ID from youtube.com/watch?v=, youtu.be/, /shorts/,
-- /embed/, or /live/ URLs. Never a URL: the frontend builds the embed URL.
ALTER TABLE posts ADD COLUMN IF NOT EXISTS youtube_id VARCHAR(11);

ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_youtube_id_check;
ALTER TABLE posts ADD CONSTRAINT posts_youtube_id_check
    CHECK (youtube_id IS NULL OR youtube_id ~ '^[A-Za-z0-9_-]{11}$');

-- A YouTube post may be video-only (no body, no local media reference).
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_not_empty;
ALTER TABLE posts ADD CONSTRAINT posts_not_empty
    CHECK (length(trim(body)) > 0 OR media_url IS NOT NULL OR youtube_id IS NOT NULL);

-- Author for admin-published posts: the admin token's subject is the string
-- "admin", not a users.id, so posts need a real row to reference. The phone
-- 'system' cannot be registered (registration normalizes to +… and rejects),
-- and the empty password_hash cannot pass bcrypt, so this account can never
-- log in — it exists only as the display name "Betva Official" in the feed.
INSERT INTO users (nickname, phone, official_name, password_hash, status)
VALUES ('Betva Official', 'system', 'Betva Official', '', 'active')
ON CONFLICT (phone) DO NOTHING;
