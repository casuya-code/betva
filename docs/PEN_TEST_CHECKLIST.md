# Pre-Launch Penetration Test Checklist

Hard launch gate: every box must be ticked and dated by the tester before go-live. Run against a staging copy of production, never against live money.

**Staging target:** frontend <https://betva-two.vercel.app>, API <https://api-production-a377a156.up.railway.app>. Topology and credentials handling: [`DEPLOYMENT.md`](DEPLOYMENT.md). Staging admin credentials were rotated on 2026-11-16 and live only in `backend/.env.staging.local` (untracked); the tester gets them out-of-band.

Scope: Go API (`/api/v1`), Astro frontend, Postgres, the Railway media volume (`/data`), Web Push, and the admin app. The tester is not the developer.

## Authentication & authorization

- [ ] Admin login rejects wrong username / wrong password with constant-time compare (no timing leak).
- [ ] **Admin 2FA:** with `ADMIN_TOTP_SECRET` set, password-only admin login is rejected, a wrong TOTP code is rejected, a current code succeeds, and the recovery code succeeds. Confirm the code field is hidden when 2FA is not configured.
- [ ] Admin JWT with `is_admin=false` cannot reach `/api/v1/admin/*`.
- [ ] User JWT cannot reach admin endpoints (401/403).
- [ ] Expired JWT is rejected; tampered signature is rejected.
- [ ] **Stateless logout:** an old token still works after `/auth/logout` — confirm this is accepted risk, or require a denylist before launch.
- [ ] Phone normalization rejects malformed input; `0712…`, `712…`, `+255712…` resolve to one account.
- [ ] Duplicate phone registration returns `409`, not a second account.
- [ ] bcrypt cost is adequate; passwords are never logged.
- [ ] Registration requires `official_name` and `accepted_terms`; a missing checkbox returns `400`.
- [ ] `consent_log` receives one row per registration with `notice_version` and timestamp.
- [ ] `PUT /me` cannot change `nickname`, `phone`, `official_name`, `status`, `balance_cents`, or `access_expires_at`.
- [ ] `PUT /me` now allows only `/media/<name>.(jpg|jpeg|png|webp|gif)` for `profile_pic`. Confirm a remote URL, `javascript:`, `data:`, `../`, or `.svg` value is rejected with `400` and cannot become a stored tracking pixel or XSS.
- [ ] Admin and user tokens are not interchangeable (claims checked, not just signature).

## Money integrity

- [ ] Double-tap placement cannot overspend (row lock verified).
- [ ] Stake is validated against `STAKE_TIERS_CENTS`; free-form and negative values rejected.
- [ ] Daily cap enforced server-side, not just client.
- [ ] One bet per user per match per market enforced by `idx_bets_one_per_market`.
- [ ] Betting is rejected when the account is inactive, access has expired, or a self-exclusion is active.
- [ ] Betting is rejected after kickoff (server-side, `Africa/Dar_es_Salaam`) and for a market not offered on the match.
- [ ] Settlement is idempotent: re-running does not double-pay.
- [ ] Correction reverses with negative ledger rows; nothing edited in place; original result stays visible.
- [ ] Postponed/cancelled match refunds every pending bet exactly once.
- [ ] Admin credit requires a reason and writes a ledger row; negative balances require the explicit flag.
- [ ] Ledger sum equals wallet balances after a scripted sequence of place / settle / correct / refund.
- [ ] `payout_multiplier_bp` and `confidence_at_placement` are snapshotted on the bet, not read live at settlement.

## Admin-only data

- [ ] `GET /api/v1/config` never returns `ACCESS_FEE_DAU_TARGET`, `MONTHLY_OPERATING_COST_CENTS`, the suggested fee, or any secret.
- [ ] `GET /api/v1/admin/analytics` is admin-only and returns `fee_plan` only to an admin token.
- [ ] `activity_days` cannot be written or read by a user token; one row per user per EAT day under concurrent requests.
- [ ] Analytics `days` outside 7–90 falls back to 30; a negative or huge value cannot widen the query.
- [ ] Official names appear only in admin user responses and the user's own `/me`; never in community, matches, stats, or public responses.
- [ ] Admin broadcast rejects a non-in-app `link`, an unknown `category`, and an over-length title/body.

## Input & injection

- [ ] SQL injection attempts on every query param and JSON field return 400/404, never 500.
- [ ] XSS payloads in nickname, post body, and media alt text are escaped on render.
- [ ] Media upload rejects non-image/non-video bytes and oversized files.
- [ ] Only `/media/*` URLs accepted in posts (no SSRF / remote embed).
- [ ] Uploaded filename is ignored; stored name is a content hash plus a sniffed extension.
- [ ] `/media/*` cannot traverse outside `MEDIA_DIR` (`../`, encoded variants, Windows `..\`).
- [ ] CSV upload rejects malformed rows with line numbers; no path traversal in file name.
- [ ] CSV `replace` is refused when bets exist for that date.
- [ ] **AI source URL:** `/admin/ai/suggest` now resolves the hostname, rejects loopback/private/link-local/multicast/CGNAT addresses (including `127.0.0.1`, `169.254.169.254`, `localhost`, and Railway internal names), pins the dial to the vetted IPs, and refuses redirects. Confirm SSRF attempts return `400` without any request reaching internal services.
- [ ] AI fetch enforces the 20s timeout and 1MB read cap; a slow-loris or huge page cannot exhaust the server.
- [ ] AI response is validated (both goals present, 0–30, confidence enum) and stored as `pending`; nothing auto-settles.
- [ ] Rejecting an AI suggestion without a reason returns `400`; an already-reviewed suggestion returns `409`.

## Rate limits & abuse

- [ ] **Rate limiting is implemented** on login (`60/5min` per IP, `10/15min` per phone), registration (`30/h` IP, `3/h` phone), admin login (`20/5min` IP), media upload and AI suggest (`30/h` per user). Attempt credential stuffing and bulk registration from one IP and from many phones; verify `429` + `Retry-After` and that a shared mobile NAT is not locked out.
- [ ] **Known gap:** there is no account lockout after repeated failures.
- [ ] Push cap (2/day) and quiet hours cannot be bypassed by repeated calls.
- [ ] A muted category still writes the inbox row but sends no push.
- [ ] Self-exclusion cannot be shortened or cancelled early; a second active period is refused.
- [ ] Report spamming: one report per user per post enforced.
- [ ] Auto-hide triggers at exactly 3 distinct reports.
- [ ] A user cannot mark another user's notification as read or unsubscribe another user's push endpoint.
- [ ] A deleted/hidden post cannot be fetched through the public feed.

## Transport, headers & browser

- [ ] HTTPS only; HTTP redirects; HSTS present on both the Vercel frontend and the Railway API.
- [ ] `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` present on every response.
- [ ] No inline script or inline style executes under the deployed CSP; all 14 built pages load external JS/CSS only.
- [ ] CORS origins are explicit in production; wildcard is absent; credentials are not allowed from unknown origins.
- [ ] Error responses do not leak stack traces, SQL, file paths, or secrets.
- [ ] Admin and user tokens in `localStorage` are accepted risk, documented, and mitigated by CSP.

## Data & privacy

- [ ] `.env`, `backend/media/`, and `frontend/scripts/` are not served or committed.
- [ ] Admin credentials come only from environment variables, never the database.
- [ ] No PII in logs or error responses (phone numbers, official names, tokens).
- [ ] Database backup files are encrypted at rest and not publicly reachable; bucket credentials exist only on the `backup` service, never on `api`.
- [ ] A user cannot read another user's wallet, history, notifications, or bets by changing an ID.
- [ ] Delete/export paths for personal data are either implemented or documented as a launch exception.

## Availability

- [ ] Restore drill (`backend/scripts/restore_test.ps1`) passes on a fresh database **and fails closed** when the throwaway database cannot be created (exit code `1`, no success message).
- [ ] A dump downloaded from the off-site bucket passes manifest SHA-256 verification and restores into a throwaway database with matching counts and ledger sum.
- [ ] Restore verification checks row counts, ledger sum, and schema completeness — not just that `pg_restore` exited `0`.
- [ ] Health endpoint `/health` returns 200.
- [ ] Graceful handling of Postgres downtime (clear 5xx, no panic) and Redis downtime (API still starts and serves).
- [ ] Gzip is applied to JSON responses; media is served with immutable cache headers.

## Developer pre-flight (2026-11-16, not a substitute for the tester sign-off)

- Admin password and JWT secret rotated; old password now returns `401`.
- API security headers verified on `/api/v1/config`; media keeps `Cache-Control: public, max-age=31536000, immutable`.
- SSRF guard and profile-picture allow-list covered by unit tests; client DNS rebinding blocked by pinning the dial to resolved public IPs.
- Full off-site restore drill passed 2026-11-16: `db/2026/11/16/betva-20261116T103504Z.dump` verified by manifest SHA-256, restored into `betva_offsite_restore_test` (1/1/1/3 counts, 225,000-cent ledger sum, 19 tables); throwaway DB and TCP proxy deleted.
- Admin TOTP verified on staging 2026-11-16: 2FA status endpoint reports `required: true`; login without a code and with code `000000` return `401`; a current code and the recovery code both return `200`.
- GROQ key set on staging; SSRF attempt to `127.0.0.1:8080` returns `400 "source_url host is not public"`; a real Groq call is attempted and its detailed error is logged server-side only.
- R2 mirror verified: a cron run wrote the same 44,782-byte dump to both buckets; `-secondary -download` verified SHA-256 and 19 table-data entries.
- Staging smoke test: user login OK; `/api/v1/config` exposes only public settings; user token gets `403` on admin analytics; tampered/missing tokens get `401`; bad `profile_pic` and invalid stake get `400`; upstream AI/Groq errors are generic to the client and logged server-side.
- Login limiter verified: 10 `401`s then `429` with `Retry-After` for one phone.
- `npm run check` passes; all 14 built pages have zero inline script tags and zero inline styles/CSS.
- CSP and the other headers verified on the live Vercel alias.
- In-memory rate limits reset on redeploy and are per-process: re-test if replicas are added.

## Sign-off

Tester: ____________  Date: ____________  Environment: ____________

Known gaps accepted for launch (list each with owner and date): ____________________________________________
