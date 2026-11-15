# Restore drill — must be run and recorded before every production deploy.

# 1. Dump the live database.
$dump = Join-Path $env:TEMP "betva-restore-$(Get-Date -Format yyyyMMddHHmmss).sql"
$env:PGPASSWORD = "betva"
& "C:\Program Files\PostgreSQL\18\bin\pg_dump.exe" -U betva -d betva -F p -f $dump
if (-not (Test-Path $dump)) { throw "dump failed" }

# 2. Create a throwaway database and restore into it.
$testDb = "betva_restore_test"
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d postgres -c "DROP DATABASE IF EXISTS $testDb;"
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d postgres -c "CREATE DATABASE $testDb OWNER betva;"
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d $testDb -f $dump | Out-Null

# 3. Verify row counts match the source for the money-critical tables.
$src = & "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d betva -t -c "SELECT (SELECT COUNT(*) FROM users) || ',' || (SELECT COUNT(*) FROM bets) || ',' || (SELECT COUNT(*) FROM ledger) || ',' || (SELECT COUNT(*) FROM matches) || ',' || (SELECT COUNT(*) FROM notifications);"
$rst = & "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d $testDb -t -c "SELECT (SELECT COUNT(*) FROM users) || ',' || (SELECT COUNT(*) FROM bets) || ',' || (SELECT COUNT(*) FROM ledger) || ',' || (SELECT COUNT(*) FROM matches) || ',' || (SELECT COUNT(*) FROM notifications);"
if ($src.Trim() -ne $rst.Trim()) { throw "COUNT MISMATCH: source=$($src.Trim()) restore=$($rst.Trim())" }
Write-Host "restore counts match: $($src.Trim())"

# 4. Verify a checksum of the ledger (money must never drift).
$srcSum = & "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d betva -t -c "SELECT COALESCE(SUM(amount_cents),0) FROM ledger;"
$rstSum = & "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d $testDb -t -c "SELECT COALESCE(SUM(amount_cents),0) FROM ledger;"
if ($srcSum.Trim() -ne $rstSum.Trim()) { throw "LEDGER SUM MISMATCH" }
Write-Host "ledger sum matches: $($srcSum.Trim())"

# 5. Clean up.
& "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U betva -d postgres -c "DROP DATABASE IF EXISTS $testDb;"
Remove-Item $dump
Write-Host "RESTORE DRILL PASSED"
