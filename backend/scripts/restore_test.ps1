# Restore drill — must be run and recorded before every production deploy.
# Restores the live database into a throwaway database and compares the
# money-critical tables. Any failure stops the script with a non-zero exit code.
#
# The role used below must be allowed to CREATE DATABASE. If it is not:
#   psql -U postgres -c "ALTER ROLE betva CREATEDB;"

$pgBin = 'C:\Program Files\PostgreSQL\18\bin'
$dump = Join-Path $env:TEMP "betva-restore-$(Get-Date -Format yyyyMMddHHmmss).sql"
$testDb = 'betva_restore_test'
$env:PGPASSWORD = 'betva'

$script:created = $false
$script:failed = $false

function Invoke-Psql {
  param(
    [Parameter(Mandatory)][string]$Database,
    [Parameter(Mandatory)][string]$Sql
  )
  $out = & "$pgBin\psql.exe" -U betva -d $Database -v ON_ERROR_STOP=1 -t -A -c $Sql 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw "psql failed on database '$Database': $Sql`n$($out -join "`n")"
  }
  return (($out | Where-Object { $_ -notmatch '^NOTICE:' }) -join "`n").Trim()
}

try {
  # 1. Dump the live database.
  & "$pgBin\pg_dump.exe" -U betva -d betva -F p -f $dump 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $dump)) { throw 'pg_dump failed' }

  # 2. Create a throwaway database and restore into it.
  $null = & "$pgBin\psql.exe" -U betva -d postgres -c "DROP DATABASE IF EXISTS $testDb;" 2>&1
  $out = & "$pgBin\psql.exe" -U betva -d postgres -c "CREATE DATABASE $testDb OWNER betva;" 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw "could not create the restore database. Grant CREATEDB first: ALTER ROLE betva CREATEDB;`n$($out -join "`n")"
  }
  $script:created = $true

  $out = & "$pgBin\psql.exe" -U betva -d $testDb -v ON_ERROR_STOP=1 -q -f $dump 2>&1
  if ($LASTEXITCODE -ne 0) {
    throw "restore into '$testDb' failed`n$($out -join "`n")"
  }

  # 3. Verify row counts match the source for the money-critical tables.
  $counts = "SELECT (SELECT COUNT(*) FROM users) || ',' || (SELECT COUNT(*) FROM bets) || ',' || (SELECT COUNT(*) FROM ledger) || ',' || (SELECT COUNT(*) FROM matches) || ',' || (SELECT COUNT(*) FROM notifications);"
  $src = Invoke-Psql -Database betva -Sql $counts
  $rst = Invoke-Psql -Database $testDb -Sql $counts
  if ([string]::IsNullOrWhiteSpace($src) -or [string]::IsNullOrWhiteSpace($rst)) {
    throw "count query returned nothing (source='$src' restore='$rst')"
  }
  if ($src -ne $rst) { throw "COUNT MISMATCH: source=$src restore=$rst" }
  if ($src -eq '0,0,0,0,0') { throw 'source database looks empty; refusing to record a drill' }
  Write-Host "restore counts match: $src"

  # 4. Verify a checksum of the ledger (money must never drift).
  $srcSum = Invoke-Psql -Database betva -Sql 'SELECT COALESCE(SUM(amount_cents), 0)::text FROM ledger;'
  $rstSum = Invoke-Psql -Database $testDb -Sql 'SELECT COALESCE(SUM(amount_cents), 0)::text FROM ledger;'
  if ([string]::IsNullOrWhiteSpace($srcSum) -or [string]::IsNullOrWhiteSpace($rstSum)) {
    throw "ledger sum query returned nothing (source='$srcSum' restore='$rstSum')"
  }
  if ($srcSum -ne $rstSum) { throw "LEDGER SUM MISMATCH: source=$srcSum restore=$rstSum" }
  Write-Host "ledger sum matches: $srcSum"

  # 5. The restored copy must also open without migration drift.
  $tables = Invoke-Psql -Database $testDb -Sql "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';"
  if ([int]$tables -lt 10) { throw "restored schema looks incomplete: $tables tables" }
  Write-Host "restored schema tables: $tables"

  Write-Host 'RESTORE DRILL PASSED'
} catch {
  $script:failed = $true
  Write-Host "RESTORE DRILL FAILED: $($_.Exception.Message)"
} finally {
  # 6. Clean up.
  if ($script:created) {
    $null = & "$pgBin\psql.exe" -U betva -d postgres -c "DROP DATABASE IF EXISTS $testDb;" 2>&1
  }
  if (Test-Path $dump) { Remove-Item $dump -Force }
}

if ($script:failed) { exit 1 }
exit 0
