# Validate current G4 sources without executing ignored historical Go files in tmp.
param(
 [ValidateSet('Unit','Integration','All')][string]$Stage = 'All',
 [string]$Image = 'sesamefs-go-integration-test:latest',
 [string]$Network = 'sesamefs_default',
 [string]$EnvFile = '.env'
)
$ErrorActionPreference = 'Stop'
$g4Root = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $g4Root
$g4Run = [guid]::NewGuid().ToString('N').Substring(0,8)
$g4Snapshot = Join-Path ([IO.Path]::GetTempPath()) "sesamefs-g4-$g4Run"
$g4Logs = Join-Path $g4Root "tmp/g4-$g4Run"
New-Item -ItemType Directory -Path $g4Snapshot,$g4Logs | Out-Null
$g4Files = & git ls-files --cached --others --exclude-standard
if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate current sources' }
foreach ($g4File in $g4Files) {
 $g4Source = Join-Path $g4Root $g4File
 if (!(Test-Path -LiteralPath $g4Source -PathType Leaf)) { continue }
 $g4Destination = Join-Path $g4Snapshot $g4File
 New-Item -ItemType Directory -Force -Path (Split-Path -Parent $g4Destination) | Out-Null
 Copy-Item -LiteralPath $g4Source -Destination $g4Destination
}
function Invoke-G4Docker([string[]]$Arguments,[string]$Log) {
 & docker @Arguments 2>&1 | Tee-Object -FilePath (Join-Path $g4Logs $Log)
 if ($LASTEXITCODE -ne 0) { throw "G4 validation failed; see $g4Logs/$Log" }
}
$g4Mounts = @('-v',"${g4Snapshot}:/build",'-v','sesamefs-g4-gocache:/root/.cache/go-build')
if ($Stage -in @('Unit','All')) {
 Invoke-G4Docker (@('run','--rm') + $g4Mounts + @($Image,'go','test','./...','-count=1','-timeout','10m')) 'unit.log'
}
if ($Stage -in @('Integration','All')) {
 Invoke-G4Docker (@('run','--rm','--network',$Network,'--env-file',$EnvFile,
  '-e','SESAMEFS_URL=http://sesamefs:8080','-e','SESAMEFS_REQUIRE_G4_EVIDENCE=1') + $g4Mounts +
  @($Image,'go','test','-tags','integration','./internal/integration','-count=1','-v','-timeout','3m',
   '-run','^TestG4CassandraMinIOPhysicalLifeCoexistence$|^TestEveryEvidenceGateIsWiredIntoTestMain$')) 'integration.log'
}
Write-Host "G4 validation passed. Logs: $g4Logs; source snapshot: $g4Snapshot"
