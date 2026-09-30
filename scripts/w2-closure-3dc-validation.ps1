# Run after w2-closure-validation.ps1 has prepared the isolated primary stack.
param([string]$Image = 'sesamefs-w2-0-evidence-go-integration-test')
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$project = 'sesamefs-w2-0-3dc'
$runner = "$project-runner-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$previousPrefix = $env:CASSANDRA_3DC_CONTAINER_PREFIX
$env:CASSANDRA_3DC_CONTAINER_PREFIX = $project
New-Item -ItemType Directory -Force tmp | Out-Null
@"
services:
  cassandra-na:
    ports: !reset []
  cassandra-eu:
    ports: !reset []
  cassandra-asia:
    ports: !reset []
"@ | Set-Content -Encoding utf8 tmp/w2-3dc-isolated.override.yaml
$compose = @('compose','-p',$project,'-f','docker-compose.cassandra-3dc.yaml','-f','tmp/w2-3dc-isolated.override.yaml')
function Invoke-Checked([string[]]$Arguments) {
 & docker @Arguments
 if ($LASTEXITCODE -ne 0) { throw "Docker failed: $($Arguments -join ' ')" }
}
function Wait-Node([string]$Node) {
 $deadline = (Get-Date).AddMinutes(15)
 do {
  $health = & docker inspect --format '{{.State.Health.Status}}' "${project}-$Node"
  if ($LASTEXITCODE -ne 0) { throw "Node unavailable: $Node" }
  if ($health -eq 'healthy') { return }
  if ((Get-Date) -gt $deadline) { throw "Node health timeout: $Node" }
  Start-Sleep -Seconds 5
 } while ($true)
}
$org = [guid]::NewGuid().ToString()
$phaseEnv = @('-e','W2_POST_HEAD_3DC_HOSTS=dc-na=cassandra-na:9042,dc-eu=cassandra-eu:9042,dc-asia=cassandra-asia:9042',
 '-e',"W2_REPAIR_3DC_ORG=$org",'-e','SESAMEFS_REQUIRE_W2_REPAIR_3DC_EVIDENCE=1')
function Invoke-Phase([string]$Phase) {
 & docker exec @phaseEnv -e "W2_REPAIR_3DC_PHASE=$Phase" $runner go test -tags integration -v -count=1 -timeout 3m -run '^TestW2RepairGuard3DC$' ./internal/integration 2>&1 |
  Tee-Object -FilePath "tmp/w2-3dc-$Phase.log"
 if ($LASTEXITCODE -ne 0) { throw "3DC phase failed: $Phase" }
}
$started = $false
$runnerStarted = $false
try {
 $started = $true
 Invoke-Checked ($compose + @('up','-d'))
 foreach ($node in @('na','eu','asia')) { Wait-Node $node }
 Invoke-Checked @('run','-d','--name',$runner,'--network',"${project}_default",'--env-file','.env.example',
  '-e','SESAMEFS_URL=http://sesamefs:8080','-v','sesamefs-w2-0-evidence_gocache:/root/.cache/go-build',$Image,'sleep','7200')
 $runnerStarted = $true
 Invoke-Checked @('network','connect','sesamefs-w2-0-evidence_default',$runner)
 Invoke-Checked @('exec','-e','CASSANDRA_HOSTS=cassandra-na:9042','-e','CASSANDRA_LOCAL_DC=dc-na',
  '-e','CASSANDRA_USERNAME=','-e','CASSANDRA_PASSWORD=','-e','CASSANDRA_REPLICATION_DCS=dc-na:1,dc-eu:1,dc-asia:1',
  $runner,'go','run','./cmd/sesamefs','migrate')
 foreach ($node in @('na','eu','asia')) { Invoke-Checked @('exec',"${project}-$node",'nodetool','disablehandoff') }
 Invoke-Checked @('stop',"${project}-na","${project}-asia")
 Invoke-Phase 'seed'
 Invoke-Checked @('start',"${project}-na","${project}-asia")
 Wait-Node 'na'; Wait-Node 'asia'
 # Mutate a disposable source copy before EACH_QUORUM read repair converges NA.
 $mutant = @"
set -euo pipefail
mkdir -p /tmp/w2-local-quorum-mutant
cp -a go.mod go.sum internal cmd /tmp/w2-local-quorum-mutant/
cd /tmp/w2-local-quorum-mutant
perl -0777 -i -pe 's/Consistency\(gocql\.EachQuorum\)\.PageSize\(256\)/Consistency(gocql.LocalQuorum).PageSize(256)/ or die "repair read mutation missed"' internal/db/publication_liveness.go
rc=0
go test -tags integration -v -count=1 -timeout 3m -run '^TestW2RepairGuard3DC$' ./internal/integration > /tmp/w2-local-quorum.log 2>&1 || rc=`$?
cat /tmp/w2-local-quorum.log
if [ "`$rc" -eq 0 ] || ! grep -q 'blind NA failed to see EU guard' /tmp/w2-local-quorum.log; then exit 1; fi
if grep -Eq 'build failed|syntax error' /tmp/w2-local-quorum.log; then exit 1; fi
echo 'PASS SEMANTIC RED: LOCAL_QUORUM REPAIR READ MISSES DURABLE EU GUARD'
"@
 $mutant = $mutant.Replace("`r`n","`n")
 & docker exec @phaseEnv -e W2_REPAIR_3DC_PHASE=readGuard $runner bash -c $mutant 2>&1 | Tee-Object tmp/w2-3dc-local-quorum-mutant.log
 if ($LASTEXITCODE -ne 0) { throw '3DC semantic negative control failed' }
 Invoke-Phase 'readGuard'
 Invoke-Phase 'promote'
 Invoke-Phase 'readPermanent'
 Invoke-Checked @('stop',"${project}-eu")
 Invoke-Phase 'unavailable'
 Invoke-Checked @('start',"${project}-eu")
 Wait-Node 'eu'
 Invoke-Phase 'cleanup'
} finally {
 if ($started) {
  # Restore hints before stopping only this named fixture. No volume deletion.
  foreach ($node in @('na','eu','asia')) { & docker start "${project}-$node" | Out-Null }
  foreach ($node in @('na','eu','asia')) {
   try { Wait-Node $node; Invoke-Checked @('exec',"${project}-$node",'nodetool','enablehandoff') }
   catch { Write-Warning $_ }
  }
  & docker @compose stop
 }
 if ($runnerStarted) { & docker rm -f $runner }
 $env:CASSANDRA_3DC_CONTAINER_PREFIX = $previousPrefix
}
