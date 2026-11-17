-- Betva migration 012: allow short "take a break" pauses.
--
-- The Profile page and the hour-long session nudge offer 3, 6 and 12 hour
-- pauses, and the API handler accepts them, but migration 004 created the table
-- with CHECK (duration_hours IN (24, 168, 720)). Every short pause therefore
-- failed at INSERT with a constraint violation, which the handler reported to
-- the user only as a generic "db error".
--
-- Widen the constraint to the full set the UI and handler already support.
-- The old set is a subset, so existing rows remain valid.
ALTER TABLE self_exclusions
    DROP CONSTRAINT IF EXISTS self_exclusions_duration_check;

ALTER TABLE self_exclusions
    ADD CONSTRAINT self_exclusions_duration_check
    CHECK (duration_hours IN (3, 6, 12, 24, 168, 720));
