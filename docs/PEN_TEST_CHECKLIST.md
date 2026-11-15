# Pre-Launch Penetration Test Checklist

Hard launch gate: every box must be ticked and dated by the tester before go-live. Run against a staging copy of production, never against live money.

## Authentication & authorization

- [ ] Admin login rejects wrong username / wrong password with constant-time compare (no timing leak).
- [ ] Admin JWT with `is_admin=false` cannot reach `/api/v1/admin/*`.
- [ ] User JWT cannot reach admin endpoints (401/403).
- [ ] Expired JWT is rejected; no refresh-token reuse.
- [ ] Phone normalization rejects malformed input (`+255…` only).
- [ ] bcrypt cost is adequate; passwords are never logged.

## Money integrity

- [ ] Double-tap placement cannot overspend (row lock verified).
- [ ] Stake is validated against `STAKE_TIERS_CENTS`; free-form rejected.
- [ ] Daily cap enforced server-side, not just client.
- [ ] One bet per user per match per market enforced by `idx_bets_one_per_market`.
- [ ] Settlement is idempotent: re-running does not double-pay.
- [ ] Correction reverses with negative ledger rows; nothing edited in place.
- [ ] Postponed match refunds every pending bet exactly once.
- [ ] Admin credit requires a reason and writes a ledger row.

## Input & injection

- [ ] SQL injection attempts on every query param and JSON field return 400/404, never 500.
- [ ] XSS payloads in nickname, post body, media alt text are escaped on render.
- [ ] Media upload rejects non-image/non-video bytes and oversized files.
- [ ] Only `/media/*` URLs accepted in posts (no SSRF / remote embed).
- [ ] CSV upload rejects malformed rows with line numbers; no path traversal in file name.

## Rate limits & abuse

- [ ] Push cap (2/day) and quiet hours cannot be bypassed by repeated calls.
- [ ] Self-exclusion cannot be shortened or cancelled early.
- [ ] Report spamming: one report per user per post enforced.
- [ ] Auto-hide triggers at exactly 3 distinct reports.

## Data & privacy

- [ ] `.env` and `media/` are not served or committed.
- [ ] Admin credentials come only from environment variables, never the database.
- [ ] No PII in logs or error responses.
- [ ] CORS origins are explicit in production.

## Availability

- [ ] Restore drill (`backend/scripts/restore_test.ps1`) passes on a fresh database.
- [ ] Health endpoint `/health` returns 200.
- [ ] Graceful handling of Postgres/Redis downtime (no panic, clear error).

## Sign-off

Tester: ____________  Date: ____________  Environment: ____________
