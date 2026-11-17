-- Betva migration 011: speed up the public trust-counter aggregation.
--
-- GET /api/v1/stats scans every settled match, ordered by settled_at, and the
-- same query also joins match_markets. Without an index that is a full table
-- scan plus a sort on every cache miss. The partial index covers exactly the
-- settled rows the query reads, and the leading column matches the ORDER BY.
CREATE INDEX IF NOT EXISTS idx_matches_settled_at
    ON matches (settled_at, id)
    WHERE settled_at IS NOT NULL;
