# Staging Deployment

**Status: ✅ live.** The API, database, and frontend are deployed and verified end to end.
This document is the operational record for the staging environment and the steps to
redeploy or promote it.

| Piece | Platform | Address |
| --- | --- | --- |
| Frontend (Astro static) | Vercel | <https://betva-two.vercel.app> |
| API (Go) | Railway | <https://api-production-a377a156.up.railway.app> |
| Postgres 18 | Railway (private network) | `postgres.railway.internal:5432` |
| Media volume | Railway volume `media` (1 GB) | mounted at `/data` |
| Off-site backups | Railway cron `backup` + bucket `betva-backups` | daily 04:00 EAT |

- Railway project: `betva` (`4b93fc0e-449f-4e94-82b8-1a2693f39710`), environment `production`
- Vercel project: `casuya-codes/betva`
- Region: **`europe-west4`** for both Railway services (closest available to Tanzania).
  `sfo` was the template default and was moved before any data was written.

> Cloudflare is **not** in front of either service yet. That is the next delivery step;
> see [Known gaps](#known-gaps).

---

## 1. What runs where

### Railway: `api`

- Built from `backend/Dockerfile` (multi-stage Go build, Alpine runtime).
- Two binaries in the image: `/app/server` (API) and `/app/migrate` (migration runner).
- Pre-deploy command: `/app/migrate` — runs before every deployment, applies only
  unapplied migrations, and blocks the deploy if a migration fails.
- Healthcheck: `GET /health` (timeout 120 s, restart on failure, max 10 retries).
- Listens on `:8080`; Railway routes the public domain to port 8080.
- `MEDIA_DIR=/data/media` on the attached volume, so uploads survive redeploys.
- One replica. No autoscaling or load test yet.

### Railway: `Postgres`

- `ghcr.io/railwayapp-templates/postgres-ssl:18`, 5 GB volume at
  `/var/lib/postgresql/data`.
- `DATABASE_URL` on `api` references `${{Postgres.DATABASE_URL}}`.
- Migrations `001`–`006` are applied; `schema_migrations` records each version.

### Railway: `backup`

- Cron service built from `backend/Dockerfile.backup` (`postgres:18-alpine` + the
  `cmd/backup` Go binary, running as `postgres`).
- Schedule `0 1 * * *` (01:00 UTC = 04:00 EAT), `restartPolicyType: NEVER`, one replica.
- Each run: `pg_dump --format=custom` → `pg_restore --list` verification (refuses a dump
  with zero table data) → SHA-256 + size manifest → upload to the `betva-backups` bucket →
  grandfather-father-son prune.
- The bucket holds `db/YYYY/MM/DD/betva-YYYYMMDDTHHMMSSZ.dump` and a matching
  `.dump.json` manifest. Retention: daily 14 days, Sunday 56 days, first-of-month 183
  days; nothing under 24 hours old is deleted.
- Credentials live only on this service. `api` has no object-storage access.
- Railway's own managed volume backups and PITR are **Pro-only**; this workspace is
  Hobby, so the bucket cron is the backup layer. See
  [Off-site backups](#4-off-site-backups).

### Redis

Not provisioned. The API attempts a connection at startup, logs
`redis connect (non-fatal)`, and continues. Nothing reads or writes Redis today, so
staging runs without it. Add a Railway Redis service and set `REDIS_URL` when a feature
actually needs it.

### Vercel: `betva`

- Astro static build (`npm run build` → `dist/`), production alias
  `betva-two.vercel.app`.
- Build-time variable `PUBLIC_API_URL` is baked into every page and exposed as
  `window.BETVA_API`.
- `PUBLIC_API_URL` is set for **Production** and **Preview** targets.
- PWA manifest, service worker, SVG favicon, PNG icons, and `favicon.ico` are served
  from `frontend/public/`.

---

## 2. Deploying the backend

Prerequisites: Railway CLI authenticated (`railway whoami`), and the directory linked
to the `api` service.

```bash
cd backend
railway link --project 4b93fc0e-449f-4e94-82b8-1a2693f39710 \
  --environment production --service api
railway up --ci --service api
```

`railway up` uploads `backend/`, builds the Dockerfile, runs `/app/migrate`, then starts
the server and waits for `/health`.

**Service configuration lives on the Railway service, not only in `railway.toml`.**
Railway's Config-as-Code is deprecated (files keep working until 2026-12-01) and was not
applied to CLI uploads, so the builder, Dockerfile path, pre-deploy command, healthcheck,
and restart policy were set directly on the service. `backend/railway.toml` is kept as
the source of truth for a future GitHub-connected deploy; if you connect the repo, verify
that Railway reads it before relying on it.

### Migrations

- SQL files live in `backend/migrations/` and are embedded into the image by
  `migrations/embed.go`.
- `cmd/migrate` creates `schema_migrations`, applies pending files in lexical order,
  each in its own transaction, and is safe to run repeatedly.
- The pre-deploy step is the normal path. To run it manually against a reachable
  database:

  ```bash
  cd backend
  DATABASE_URL='postgres://…' go run ./cmd/migrate
  ```

- Migrations are **forward-only**. There are no down migrations; recovery from a bad
  migration is a restore, not a rollback.

---

## 3. Deploying the frontend

```bash
cd frontend
vercel link --yes --project betva
vercel env add PUBLIC_API_URL production --value "https://api-production-a377a156.up.railway.app" --force
vercel env add PUBLIC_API_URL preview    --value "https://api-production-a377a156.up.railway.app" --force
vercel --prod --yes
```

The production alias is printed at the end (`betva-two.vercel.app`). A preview URL is
also produced per deployment; add it to `CORS_ORIGINS` if you need to test previews
against the staging API.

### CORS

The API only accepts the origins listed in `CORS_ORIGINS`. After a domain change:

```bash
railway variable set --service api \
  "CORS_ORIGINS=https://betva-two.vercel.app,http://localhost:4321,http://127.0.0.1:4321"
```

Setting a variable triggers a redeploy. Verified preflight returns
`access-control-allow-origin: https://betva-two.vercel.app` for `POST /api/v1/bets`.

---

## 4. Off-site backups

The database is dumped every day to a Railway storage bucket, outside the Postgres service
and outside the database volume.

| Item | Value |
| --- | --- |
| Service | `backup` (`d1991f7d-3c97-45e1-a73e-9b005af1663a`) |
| Image | `backend/Dockerfile.backup` |
| Schedule | `0 1 * * *` UTC (04:00 EAT) |
| Bucket | `betva-backups` (`06597326-ad58-422f-b9d9-48df345276b9`), name `betva-backups-yulkgd-4ic` |
| Endpoint | `https://t3.storageapi.dev`, region `auto`, virtual-host style |
| Layout | `db/YYYY/MM/DD/betva-YYYYMMDDTHHMMSSZ.dump` + `.dump.json` |
| Retention | daily 14 days, Sunday 56 days, first-of-month 183 days |

### Deploying or changing the job

```bash
cd backend
railway link --project 4b93fc0e-449f-4e94-82b8-1a2693f39710 \
  --environment production --service backup
railway up --ci --service backup
```

`railway up <path>` fails with `prefix not found`; run it from `backend/` without a path
argument. Change `cronSchedule` or the Dockerfile path through the service settings or the
Railway API — Config-as-Code is deprecated.

### Variables (service `backup` only)

| Variable | Value |
| --- | --- |
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` |
| `BACKUP_S3_BUCKET` | `betva-backups-yulkgd-4ic` |
| `BACKUP_S3_ENDPOINT` | `https://t3.storageapi.dev` |
| `BACKUP_S3_REGION` | `auto` |
| `BACKUP_PREFIX` | `db` |
| `BACKUP_DAILY_KEEP_DAYS` | `14` |
| `BACKUP_WEEKLY_KEEP_DAYS` | `56` |
| `BACKUP_MONTHLY_KEEP_DAYS` | `183` |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | bucket credentials |

### Operating the job

Run these with the service environment (locally: `backend/.env` plus bucket credentials,
or `railway run --service backup <command>`):

```bash
go run ./cmd/backup                                        # take, verify, upload, prune now
go run ./cmd/backup -list                                  # stored dumps, newest first
go run ./cmd/backup -download <key> -out ./restore.dump    # verifies size + SHA-256
go run ./cmd/backup -delete <key>                          # delete one dump + manifest
```

`-download` refuses a size or SHA-256 mismatch against the manifest, so a corrupt download
cannot be mistaken for a good backup. A run exits non-zero on any failure, which marks the
Railway cron deployment failed.

### Verified 2026-11-16

- A temporary `*/5 * * * *` schedule produced
  `db/2026/11/16/betva-20261116T103504Z.dump` (44,353 bytes, 19 tables) at 10:35:04 UTC;
  logs showed `uploaded … 19 tables` and `done in 550ms`.
- Downloading that object back verified its SHA-256 and 19 table-data entries with
  `pg_restore --list`. The schedule was then restored to `0 1 * * *`.
- Restoring the downloaded dump is covered in
  [`RESTORE_RUNBOOK.md`](RESTORE_RUNBOOK.md#5-restoring-from-the-off-site-bucket).

### Media

The `media` volume is attached to `api`, and a Railway volume mounts to one service, so the
cron cannot read it. Media is backed up manually until it moves to R2:

```bash
railway volume files download /media ./media-backup --volume media
```

---

## 5. Environment variables

Secrets are stored in Railway/Vercel, never in the repo. `backend/.env.example` lists
every name.

### Railway (`api`)

| Variable | Staging value / source | Notes |
| --- | --- | --- |
| `ENV` | `production` | Enables production behaviour |
| `PORT` | `8080` | Railway routes the domain here |
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` | Private network |
| `JWT_SECRET` | generated for staging | 48 random hex chars; rotated 2026-11-16; rotate = every session logs out |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | rotated 2026-11-16 | New values in `backend/.env.staging.local` (untracked). Change again before public launch |
| `CORS_ORIGINS` | Vercel alias + localhost | Exact origins only |
| `ACCESS_FEE_CENTS` | `500000` | 5,000 TZS flat monthly rate |
| `PAYOUT_MULTIPLIER_BP` | `15000` | 1.50× return on a win |
| `MEDIA_DIR` | `/data/media` | Railway volume |
| `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` / `VAPID_SUBJECT` | from the operator's local `.env` | Push enabled |
| `PUSH_DAILY_CAP` | `2` | Enforced server-side |
| `QUIET_START_HOUR` / `QUIET_END_HOUR` | `22` / `7` | EAT |
| `GROQ_API_KEY` | **not set** | AI suggestions disabled until a key is added |
| `ACCESS_FEE_DAU_TARGET` / `MONTHLY_OPERATING_COST_CENTS` | **not set** | Fee planner shows unconfigured |

`REDIS_URL` is not set; the connection failure is non-fatal.

### Vercel (`betva`)

| Variable | Target | Value |
| --- | --- | --- |
| `PUBLIC_API_URL` | Production, Preview | Railway API origin |

---

## 6. Verification checklist

Run these after every deploy. All were run on 2026-11-16.

| Check | Expected |
| --- | --- |
| `GET /health` | `{"status":"ok","service":"betva-api"}` |
| `GET /api/v1/config` | `access_fee_cents: 500000`, `push_enabled: true`, stake tiers, markets |
| Admin login + `GET /api/v1/admin/me` | `env: production` |
| `GET /api/v1/admin/analytics?days=7` | 7 day rows, no `db error` |
| CORS preflight from the Vercel origin | `204` with the alias echoed |
| Security headers on API and frontend | CSP, HSTS, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, `Permissions-Policy` present |
| Login rate limit | 10 `401`s for one phone, then `429` with `Retry-After` |
| Register → activate → credit → upload CSV → bet → settle | Bet `win`, payout `75000`, balance `225000` |
| `GET /api/v1/stats` | Settled tips counted, losses visible |
| Frontend home | Match cards render, settled markets show ✓, track record matches the API |
| Restore drill | See [section 7](#7-restore-drill) |

The E2E test created one staging user (`+255711161006`), one fixture
(Simba SC vs Yanga SC, settled 2–0, HT 1–0), and three ledger rows. Delete or keep as
staging fixtures; they are not production data.

---

## 7. Restore drill

A staging dump was restored into a throwaway database on the same Postgres instance and
verified, then dropped:

| Date | Environment | Result | Users | Matches | Bets | Ledger rows | Ledger sum | Migrations | Tables |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-11-16 | Railway staging → throwaway DB | **Pass** | 1 | 1 | 1 | 3 | 225,000 cents | 6 | 19 |

Procedure and failure handling: [`docs/RESTORE_RUNBOOK.md`](RESTORE_RUNBOOK.md).
The public TCP proxy used for the drill was deleted immediately afterwards.

---

## 8. Rollback

- **API:** Railway → service `api` → Deployments → redeploy the previous successful
  deployment. The database is not rolled back.
- **Frontend:** Vercel → project `betva` → Deployments → promote the previous
  production deployment, or `vercel rollback`.
- **Schema:** forward-only. If a migration is wrong, restore the database from a
  verified backup; do not hand-edit `schema_migrations`.

---

## 9. Known gaps

Launch blockers or follow-ups, in order:

1. **Cloudflare is not configured.** No WAF, no edge cache in Dar es Salaam/Mombasa, no
   R2 media. The Vercel alias and Railway API are public directly.
2. **RPO is up to 24 hours.** Daily off-site dumps are live (section 4), but Railway
   managed volume backups and point-in-time recovery are Pro-only and this workspace is
   Hobby. Near-zero RPO for wallets needs Pro PITR or a WAL-shipping setup. Media on the
   volume is not covered by the database dump and is backed up manually.
3. **Admin credentials rotated 2026-11-16.** New values live only in
   `backend/.env.staging.local` (untracked) and Railway; the old development password is
   rejected. Rotating `JWT_SECRET` logged every session out. Rotate again before public
   launch and store the values in a password manager.
4. **No Redis.** Fine today because nothing uses it; required before any feature that
   assumes it.
5. **Single replica, no load test.** Kickoff-scale behaviour is unmeasured.
6. **Latency:** from Tanzania, a cold request to `europe-west4` measured ~0.70 s total
   (TCP ~0.17 s, TLS ~0.38 s, TTFB ~0.31 s). Keep-alive traffic is much lower. Real
   mid-range Android over 3G is still unmeasured; Cloudflare in front is the planned fix.
7. **Config-as-Code deprecation.** `railway.toml` stops working on 2026-12-01. Migrate to
   `.railway/railway.ts` or keep configuring the service through the dashboard/API.
8. **Security gate not fully run.** `docs/PEN_TEST_CHECKLIST.md` still needs an independent tester. Security headers, a strict CSP, basic rate limiting, and credential-rotation are done; admin 2FA is not.
9. **AI results disabled.** No `GROQ_API_KEY` on staging.
10. **Fee planner unconfigured.** Set `ACCESS_FEE_DAU_TARGET` and
    `MONTHLY_OPERATING_COST_CENTS` when the operator has real numbers.
11. **Container runs as root.** The image has no `USER` because Railway volumes mount
    root-owned; add a startup `chown` + privilege drop before production.
