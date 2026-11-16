# Restore Runbook

**Purpose:** recover Betva after data loss, corruption, or a bad migration — and prove the recovery works before launch.

**Owner:** the operator. **Gate:** a full restore drill against staging is a hard launch blocker, alongside the penetration test.

---

## 1. What must be recovered

| Data | Table(s) | Criticality |
| --- | --- | --- |
| Users and access state | `users`, `consent_log`, `self_exclusions` | Critical |
| Wallet movements | `ledger` | Critical |
| Bets and settlements | `bets`, `results_archive` | Critical |
| Matches and imports | `matches`, `match_imports` | High |
| Community | `posts`, `post_reports` | Medium |
| Notifications | `notifications`, `notification_prefs`, `push_subscriptions`, `push_delivery_log` | Medium |
| Analytics | `activity_days` | High (financial decisions depend on it) |
| AI review | `result_suggestions` | Medium |
| Media files | `MEDIA_DIR` on disk | Medium |

**Targets:** RTO ≤ 1 hour for critical data; RPO ≤ 1 minute for wallets (requires continuous archiving / PITR — not configured yet).

---

## 2. Prerequisites

- PostgreSQL client tools (`pg_dump`, `pg_restore`, `psql`) matching the server major version.
- A role that can **create databases** for the drill:
  ```sql
  ALTER ROLE betva CREATEDB;
  ```
  The local `betva` role does **not** have this by default. Without it the drill fails closed (exit `1`) — that is correct behaviour, not a bug.
- Credentials supplied through the environment, never typed into a script:
  ```powershell
  $env:PGHOST     = '...'
  $env:PGPORT     = '5432'
  $env:PGUSER     = '...'
  $env:PGPASSWORD = '...'
  $env:PGDATABASE = 'betva'
  ```
- Enough disk for a full dump (roughly the database size, uncompressed for the drill).

---

## 3. Drill procedure (staging, before launch)

The automated drill is `backend/scripts/restore_test.ps1`. It dumps the live database, restores into a throwaway database, and compares the money-critical tables. It **fails closed**: if the throwaway database cannot be created, the counts are empty, or any value differs, it prints `RESTORE DRILL FAILED` and exits `1`.

> **Known limitation:** the script currently hardcodes `C:\Program Files\PostgreSQL\18\bin`, the local `betva` role, and the `betva` database. Before the staging run, parameterise `$pgBin`, `PGUSER`, and the source database name, or run the equivalent commands by hand.

### 3.1 Automated

```powershell
cd backend
.\scripts\restore_test.ps1
```

Expected output on success:

```
restore counts match: <users>,<bets>,<ledger>,<matches>,<notifications>
ledger sum matches: <cents>
restored schema tables: <n>
RESTORE DRILL PASSED
```

Record the three values in the drill log below.

### 3.2 Manual (if the script cannot run)

1. **Take a fresh dump.**
   ```bash
   pg_dump -F c -f betva-$(date +%Y%m%d-%H%M%S).dump "$DATABASE_URL"
   ```
2. **Create a new database.** Never restore over the live one.
   ```bash
   createdb -T template0 betva_restore_test
   ```
3. **Restore.**
   ```bash
   pg_restore --no-owner --no-privileges -d betva_restore_test betva-YYYYMMDD-HHMMSS.dump
   ```
4. **Verify counts.**
   ```sql
   SELECT
     (SELECT COUNT(*) FROM users)       AS users,
     (SELECT COUNT(*) FROM bets)        AS bets,
     (SELECT COUNT(*) FROM ledger)      AS ledger,
     (SELECT COUNT(*) FROM matches)     AS matches,
     (SELECT COUNT(*) FROM notifications) AS notifications;
   ```
   Compare with the source. Any mismatch is a failed drill.
5. **Verify the ledger sum.**
   ```sql
   SELECT COALESCE(SUM(amount_cents), 0) FROM ledger;
   ```
   The number must match the source exactly. Money must never drift.
6. **Verify the schema.**
   ```sql
   SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';
   ```
   Expect at least the 18 tables created by migrations `001`–`006`.
7. **Spot-check the last 7 days.**
   ```sql
   SELECT COUNT(*) FROM bets WHERE placed_at >= NOW() - INTERVAL '7 days';
   SELECT COUNT(*) FROM ledger WHERE created_at >= NOW() - INTERVAL '7 days';
   ```
8. **Drop the throwaway database.**
   ```bash
   dropdb betva_restore_test
   ```

### 3.3 Promote (only after a verified restore)

1. Put the API into maintenance mode (stop accepting writes) or scale it to zero.
2. Rename the broken database aside; never overwrite it before the restore is verified.
3. Restore into the production database name, or point `DATABASE_URL` at the verified copy.
4. Start the API, check `/health`, then place and settle one test bet on a staging account.
5. Re-enable traffic and watch error rates for 30 minutes.
6. Record who restored, when, why, the backup identifier, and the verification values.

---

## 4. What to do when the drill fails

| Symptom | Cause | Fix |
| --- | --- | --- |
| `could not create the restore database` | Role lacks `CREATEDB` | `ALTER ROLE <role> CREATEDB;` as a superuser |
| `COUNT MISMATCH` | Dump incomplete, or source changed mid-dump | Re-run against a quiet database; investigate disk and `pg_dump` errors |
| `LEDGER SUM MISMATCH` | Money data corrupted or partially restored | Stop. Do not promote. Compare ledger rows and escalate |
| `restored schema looks incomplete` | Wrong dump or missing migrations | Check the dump timestamp; re-apply migrations `001`–`006` to a fresh DB |
| `source database looks empty` | Wrong `DATABASE_URL` | Point at the real database; an empty source can never count as a pass |

A drill that has never failed is not evidence. Verify the failing path once (remove `CREATEDB`, confirm exit `1`) and record it.

---

## 5. Drill log

| Date | Environment | Result | Users | Bets | Ledger rows | Ledger sum (cents) | Tables | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-11-16 | Local Postgres 18, throwaway DB | **Pass** | 5 | 6 | 17 | 4,750,000 | 18 | Failing path also verified: without `CREATEDB` the script exits `1` and prints `RESTORE DRILL FAILED`. |
| Before launch | Staging (Railway) | **Pending** | — | — | — | — | — | Hard launch blocker. Run against a restored staging copy. |

---

## 6. Known gaps before the staging drill

- [ ] `restore_test.ps1` is local-only (hardcoded binary path, role, and database). Parameterise it.
- [ ] No automated daily backups or PITR configured (Railway managed backups are ⬜).
- [ ] No off-site copy in Cloudflare R2 (⬜).
- [ ] Media files are on local disk and are **not** covered by the Postgres dump (⬜). Back up `MEDIA_DIR` separately or move media to R2.
- [ ] No second person has independently verified a restore (⬜).
