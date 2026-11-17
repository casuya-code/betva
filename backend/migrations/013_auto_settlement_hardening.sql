-- Betva migration 013: make match settlement reliable.
--
-- Before this migration a match could be shown as FINISHED while still having
-- no score and no settled bets. Two paths created that state:
--   1. the hourly scheduler bulk-set status='finished' for every past match,
--      then skipped settlement when no score/approved suggestion existed;
--   2. the admin status dropdown allowed status='finished' directly.
-- Only handleAdminSettleMatch writes settled_at, scores and bet outcomes, so a
-- 'finished' match with settled_at IS NULL is always a broken, stuck row.
--
-- Repair existing stuck rows. 'awaiting_result' is an honest state: the game
-- is over, betting is closed (is_open is time-based, not status-based), but a
-- result has not been recorded yet. Only settlement may set 'finished'.
UPDATE matches
SET status = 'awaiting_result', updated_at = NOW()
WHERE status = 'finished'
  AND settled_at IS NULL;

-- Speed up the settlement scheduler's candidate scan over unsettled matches.
CREATE INDEX IF NOT EXISTS idx_matches_unsettled_kickoff
    ON matches (match_date, kickoff_time)
    WHERE settled_at IS NULL;
