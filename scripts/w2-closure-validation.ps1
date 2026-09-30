# Native Windows entrypoint; every test and build runs inside Docker.
param(
 [ValidateSet('Wire','Rollout','Regression','Mutations','Gates')][string]$Stage = 'Wire',
 [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$project = 'sesamefs-w2-0-evidence'
$image = "$project-go-integration-test"
$previousEnvFile = $env:ENV_FILE
$env:ENV_FILE = '.env.example'
New-Item -ItemType Directory -Force tmp | Out-Null
$override = Join-Path $projectRoot 'tmp/w2-isolated.override.yaml'
@"
services:
  sesamefs:
    ports: !reset []
    environment:
      GC_ENABLED: 'false'
  sesamefs-node-2:
    environment:
      GC_ENABLED: 'false'
  sesamefs-node-3:
    environment:
      GC_ENABLED: 'false'
  cassandra:
    ports: !reset []
  minio:
    ports: !reset []
  frontend:
    ports: !reset []
"@ | Set-Content -LiteralPath $override -Encoding utf8
$compose = @('compose','-p',$project,'-f','docker-compose.yaml','-f',$override,'--profile','test')
function Invoke-DockerChecked([string[]]$Arguments) {
 & docker @Arguments
 if ($LASTEXITCODE -ne 0) { throw "Docker command failed ($LASTEXITCODE): $($Arguments -join ' ')" }
}
function Invoke-Evidence([string[]]$Arguments,[string]$Log) {
 & docker @Arguments 2>&1 | Tee-Object -FilePath $Log
 if ($LASTEXITCODE -ne 0) { throw "Evidence failed ($LASTEXITCODE); see $Log" }
}
$scannerEnabled = $false
try {
 $up = @('up','-d')
 if (!$SkipBuild) { $up += '--build' }
 Invoke-DockerChecked ($compose + $up + @('sesamefs','sesamefs-node-2','sesamefs-node-3','frontend'))
 if (!$SkipBuild) { Invoke-DockerChecked @('build','-f','Dockerfile.gotest','-t',$image,'.') }
 $run = @('run','--rm','--network',"${project}_default",'--env-file','.env.example',
   '-e','SESAMEFS_URL=http://sesamefs:8080','-e','SESAMEFS_URL_2=http://sesamefs-node-2:8080',
   '-e','SESAMEFS_URL_3=http://sesamefs-node-3:8080','-e','SESAMEFS_PROXY_URL=http://frontend:80',
   '-v',"${project}_gocache:/root/.cache/go-build")
 switch ($Stage) {
  'Wire' {
   Invoke-Evidence ($run + @('-e','SESAMEFS_REQUIRE_W2_CLOSURE_EVIDENCE=1',$image,'go','test','-tags','integration',
      '-v','-count=1','-timeout','8m','-run','^TestW2(NativeHEAD|ProcessKill|ClosureEvidence)|^TestEveryEvidenceGateIsWiredIntoTestMain$','./internal/integration')) 'tmp/w2-wire-crash.log'
  }
  'Rollout' {
   $baseline = '50c50903e7ac49c04ef36f460dccc35ce122dfd6'
   & git archive --format=tar --output=tmp/w2-legacy-baseline.tar $baseline
   if ($LASTEXITCODE -ne 0) { throw 'Could not archive pinned pre-#239 baseline' }
   "$baseline $(Get-FileHash tmp/w2-legacy-baseline.tar -Algorithm SHA256 | Select-Object -ExpandProperty Hash)" |
     Set-Content -Encoding utf8 tmp/w2-legacy-baseline.manifest
   Invoke-Evidence ($run + @('-v',"${projectRoot}/tmp:/build/tmp",$image,'bash','scripts/w2-closure-rollout-validation.sh')) 'tmp/w2-rollout.log'
  }
  'Regression' {
   # The supported full integration profile needs one local scanner. Enable
   # only this project's primary for its test, then restore it in finally.
   @"
services:
  sesamefs:
    environment:
      GC_ENABLED: 'true'
"@ | Set-Content -Encoding utf8 tmp/w2-scanner.override.yaml
   $scannerEnabled = $true
   Invoke-DockerChecked ($compose + @('-f','tmp/w2-scanner.override.yaml','up','-d','sesamefs'))
   Invoke-Evidence ($compose + @('run','--rm','--no-deps','go-integration-test')) 'tmp/w2-integration-full.log'
  }
  'Gates' {
   Invoke-Evidence ($run + @($image,'bash','scripts/w2-closure-gate-validation.sh')) 'tmp/w2-closure-gates.log'
  }
  'Mutations' {
   # Disposable image only; mutation script never mounts or edits host source.
   Invoke-Evidence ($run + @($image,'bash','scripts/w2-publication-continuity-mutation-validation.sh')) 'tmp/w2-mechanism-mutations.log'
  }
 }
} finally {
 if ($scannerEnabled) { Invoke-DockerChecked ($compose + @('up','-d','sesamefs')) }
 $env:ENV_FILE = $previousEnvFile
}
