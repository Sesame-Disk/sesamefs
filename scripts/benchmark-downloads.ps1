#!/usr/bin/env pwsh
# ============================================================================
# Download Performance Benchmark Script
# Tests all download paths and compares throughput
# ============================================================================

param(
    [string]$BaseURL = "http://localhost:3000",
    [string]$Token = "dev-token-admin",
    [string]$RepoID = "59134bb5-ae88-4af0-8a43-bb4547d4ac73",
    [string]$FilePath = "/archive XXX-big.zip",
    [string]$ShareToken = "",   # Will be created if empty
    [string]$OutputDir = "$env:TEMP\sesamefs-bench",
    [int]$Runs = 1              # Number of runs per test (for averaging)
)

$ErrorActionPreference = "Continue"
$ProgressPreference = "SilentlyContinue"  # Disable PowerShell progress bar (huge perf overhead)

# ============================================================================
# Helpers
# ============================================================================

function Format-Size([long]$bytes) {
    if ($bytes -ge 1GB) { return "{0:N2} GB" -f ($bytes / 1GB) }
    if ($bytes -ge 1MB) { return "{0:N2} MB" -f ($bytes / 1MB) }
    return "{0:N2} KB" -f ($bytes / 1KB)
}

function Format-Speed([long]$bytes, [double]$seconds) {
    if ($seconds -le 0) { return "N/A" }
    $mbps = ($bytes / 1MB) / $seconds
    return "{0:N1} MB/s" -f $mbps
}

function Write-Header([string]$text) {
    $line = "=" * 70
    Write-Host ""
    Write-Host $line -ForegroundColor Cyan
    Write-Host "  $text" -ForegroundColor Cyan
    Write-Host $line -ForegroundColor Cyan
}

function Write-Result([string]$label, [double]$seconds, [long]$bytes) {
    $speed = Format-Speed $bytes $seconds
    $size = Format-Size $bytes
    Write-Host ("  {0,-35} {1,10}  {2,12}  {3,12}" -f $label, ("{0:N1}s" -f $seconds), $size, $speed) -ForegroundColor Green
}

# ============================================================================
# Setup
# ============================================================================

Write-Header "SesameFS Download Benchmark"
Write-Host "  Base URL:   $BaseURL"
Write-Host "  Repo:       $RepoID"
Write-Host "  File:       $FilePath"
Write-Host "  Output dir: $OutputDir"
Write-Host "  Runs:       $Runs"
Write-Host ""

# Create output directory
if (!(Test-Path $OutputDir)) { New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null }

$headers = @{ Authorization = "Token $Token" }
$encodedPath = [System.Uri]::EscapeDataString($FilePath)  # Full encode for query params (e.g. ?p=...)
# Path-style encode: keep slashes, only encode filename parts (for URL paths like /lib/.../file/...)
$pathEncodedPath = "/" + ($FilePath.TrimStart("/").Split("/") | ForEach-Object { [System.Uri]::EscapeDataString($_) }) -join "/"
$filename = [System.IO.Path]::GetFileName($FilePath)

# ============================================================================
# Pre-flight: Verify connectivity
# ============================================================================

Write-Host "  Pre-flight check..." -NoNewline
try {
    $repos = Invoke-RestMethod -Uri "$BaseURL/api2/repos/" -Headers $headers -TimeoutSec 10
    Write-Host " OK ($($repos.Count) repos)" -ForegroundColor Green
} catch {
    Write-Host " FAILED" -ForegroundColor Red
    Write-Host "  Error: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# ============================================================================
# Test 1: Seafhttp Download (streamFileFromBlocks - WITH PREFETCHING)
# ============================================================================

Write-Header "Test 1: Seafhttp Download (prefetch pipeline)"
Write-Host "  Route: GET /seafhttp/files/{token}/{filename}"
Write-Host "  Code:  streamFileFromBlocks() - prefetch + batch resolve + 4MB buffer"
Write-Host ""

$seafhttpTimes = @()
for ($r = 1; $r -le $Runs; $r++) {
    Write-Host "  Run $r/$Runs... " -NoNewline

    # Get fresh download token each run
    $downloadURL = Invoke-RestMethod -Uri "$BaseURL/api2/repos/$RepoID/file/download-link/?p=$encodedPath" -Headers $headers
    # Replace 127.0.0.1 with localhost if needed
    $downloadURL = $downloadURL -replace "http://127\.0\.0\.1:", "http://localhost:"
    # URL-encode spaces in the filename portion for curl
    $downloadURL = $downloadURL -replace " ", "%20"

    $outFile = "$OutputDir\bench-seafhttp-$r.tmp"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()

    try {
        curl.exe -s --fail --globoff -o $outFile "$downloadURL"
        $sw.Stop()
        if (Test-Path $outFile) {
            $fileSize = (Get-Item $outFile).Length
            $elapsed = $sw.Elapsed.TotalSeconds
            $seafhttpTimes += [PSCustomObject]@{ Seconds = $elapsed; Bytes = $fileSize }
            Write-Result "Seafhttp #$r" $elapsed $fileSize
        } else {
            Write-Host "  FAILED: no output file" -ForegroundColor Red
        }
    } catch {
        $sw.Stop()
        Write-Host "  FAILED: $($_.Exception.Message)" -ForegroundColor Red
    }

    if (Test-Path $outFile) { Remove-Item $outFile -Force }
}

# ============================================================================
# Test 2: Fileview Raw Download (batch resolve + 4MB buffer, NO prefetch)
# ============================================================================

Write-Header "Test 2: Fileview Raw Download (no prefetch)"
Write-Host "  Route: GET /lib/{repo_id}/file{path}?raw=1&token={auth}"
Write-Host "  Code:  ServeRawFile() - batch resolve + 4MB buffer (sequential blocks)"
Write-Host ""

$fileviewTimes = @()
for ($r = 1; $r -le $Runs; $r++) {
    Write-Host "  Run $r/$Runs... " -NoNewline

    $fileviewURL = "$BaseURL/lib/$RepoID/file$($pathEncodedPath)?raw=1"
    $outFile = "$OutputDir\bench-fileview-$r.tmp"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()

    try {
        curl.exe -s --fail --globoff -L -o $outFile -H "Authorization: Token $Token" "$fileviewURL"
        $sw.Stop()
        if (Test-Path $outFile) {
            $fileSize = (Get-Item $outFile).Length
            $elapsed = $sw.Elapsed.TotalSeconds
            $fileviewTimes += [PSCustomObject]@{ Seconds = $elapsed; Bytes = $fileSize }
            Write-Result "Fileview #$r" $elapsed $fileSize
        } else {
            Write-Host "  FAILED: no output file" -ForegroundColor Red
        }
    } catch {
        $sw.Stop()
        Write-Host "  FAILED: $($_.Exception.Message)" -ForegroundColor Red
    }

    if (Test-Path $outFile) { Remove-Item $outFile -Force }
}

# ============================================================================
# Test 3: Share Link Raw Download (batch resolve + 4MB buffer, NO prefetch)
# ============================================================================

Write-Header "Test 3: Share Link Raw Download (no prefetch)"
Write-Host "  Route: GET /d/{share_token}/?raw=1"
Write-Host "  Code:  serveSharedFileRaw() - batch resolve + 4MB buffer (sequential)"
Write-Host ""

# Create a share link if not provided
if ([string]::IsNullOrEmpty($ShareToken)) {
    Write-Host "  Creating share link..." -NoNewline
    try {
        $body = @{ path = $FilePath; permissions = "download"; repo_id = $RepoID } | ConvertTo-Json
        $sl = Invoke-RestMethod -Uri "$BaseURL/api/v2.1/share-links/" -Method Post -Headers $headers -ContentType "application/json" -Body $body
        $ShareToken = $sl.token
        Write-Host " OK (token: $ShareToken)" -ForegroundColor Green
    } catch {
        Write-Host " FAILED: $($_.Exception.Message)" -ForegroundColor Red
    }
}

$sharelinkTimes = @()
if ($ShareToken) {
    for ($r = 1; $r -le $Runs; $r++) {
        Write-Host "  Run $r/$Runs... " -NoNewline

        $shareURL = "$BaseURL/d/$($ShareToken)/?raw=1"
        $outFile = "$OutputDir\bench-share-$r.tmp"
        $sw = [System.Diagnostics.Stopwatch]::StartNew()

        try {
            curl.exe -s --fail --globoff -o $outFile "$shareURL"
            $sw.Stop()
            if (Test-Path $outFile) {
                $fileSize = (Get-Item $outFile).Length
                $elapsed = $sw.Elapsed.TotalSeconds
                $sharelinkTimes += [PSCustomObject]@{ Seconds = $elapsed; Bytes = $fileSize }
                Write-Result "ShareLink #$r" $elapsed $fileSize
            } else {
                Write-Host "  FAILED: no output file" -ForegroundColor Red
            }
        } catch {
            $sw.Stop()
            Write-Host "  FAILED: $($_.Exception.Message)" -ForegroundColor Red
        }

        if (Test-Path $outFile) { Remove-Item $outFile -Force }
    }
} else {
    Write-Host "  SKIPPED (no share token)" -ForegroundColor Yellow
}

# ============================================================================
# Test 4: Seafhttp Download via dl=1 redirect (through fileview → seafhttp)
# ============================================================================

Write-Header "Test 4: Fileview dl=1 Redirect (redirects to seafhttp)"
Write-Host "  Route: GET /lib/{repo_id}/file{path}?dl=1 -> 302 -> /seafhttp/files/{token}/{name}"
Write-Host "  Code:  redirectToDownload() -> streamFileFromBlocks() (WITH prefetch)"
Write-Host ""

$dlRedirectTimes = @()
for ($r = 1; $r -le $Runs; $r++) {
    Write-Host "  Run $r/$Runs... " -NoNewline

    $dlURL = "$BaseURL/lib/$RepoID/file$($pathEncodedPath)?dl=1"
    $outFile = "$OutputDir\bench-dl-redirect-$r.tmp"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()

    try {
        # -L follows 302 redirects automatically
        curl.exe -s --fail --globoff -L -o $outFile -H "Authorization: Token $Token" "$dlURL"
        $sw.Stop()
        if (Test-Path $outFile) {
            $fileSize = (Get-Item $outFile).Length
            $elapsed = $sw.Elapsed.TotalSeconds
            $dlRedirectTimes += [PSCustomObject]@{ Seconds = $elapsed; Bytes = $fileSize }
            Write-Result "dl=1 Redirect #$r" $elapsed $fileSize
        } else {
            Write-Host "  FAILED: no output file" -ForegroundColor Red
        }
    } catch {
        $sw.Stop()
        Write-Host "  FAILED: $($_.Exception.Message)" -ForegroundColor Red
    }

    if (Test-Path $outFile) { Remove-Item $outFile -Force }
}

# ============================================================================
# Summary
# ============================================================================

Write-Header "RESULTS SUMMARY"

Write-Host ""
Write-Host ("  {0,-35} {1,10}  {2,12}  {3,12}  {4}" -f "METHOD", "TIME", "SIZE", "SPEED", "FEATURES") -ForegroundColor White
Write-Host ("  {0}" -f ("-" * 95)) -ForegroundColor DarkGray

function Show-Summary([string]$name, [array]$times, [string]$features) {
    if ($times.Count -eq 0) {
        Write-Host ("  {0,-35} {1}" -f $name, "SKIPPED/FAILED") -ForegroundColor DarkYellow
        return @{ AvgSeconds = 0; AvgSpeed = 0 }
    }
    $avgSec = ($times | Measure-Object -Property Seconds -Average).Average
    $avgBytes = ($times | Measure-Object -Property Bytes -Average).Average
    $speedMBs = if ($avgSec -gt 0) { ($avgBytes / 1MB) / $avgSec } else { 0 }

    $color = if ($speedMBs -ge 120) { "Green" } elseif ($speedMBs -ge 90) { "Yellow" } else { "Red" }
    Write-Host ("  {0,-35} {1,10}  {2,12}  {3,12}  {4}" -f $name, ("{0:N1}s" -f $avgSec), (Format-Size $avgBytes), ("{0:N1} MB/s" -f $speedMBs), $features) -ForegroundColor $color

    return @{ AvgSeconds = $avgSec; AvgSpeed = $speedMBs }
}

$r1 = Show-Summary "1. Seafhttp (prefetch)"    $seafhttpTimes   "prefetch+batch+4MB+flush/4"
$r2 = Show-Summary "2. Fileview raw"            $fileviewTimes   "batch+4MB+flush/4"
$r3 = Show-Summary "3. Share link raw"          $sharelinkTimes  "batch+4MB+flush/4"
$r4 = Show-Summary "4. dl=1 -> seafhttp"        $dlRedirectTimes "redirect+prefetch+batch+4MB"

Write-Host ""
Write-Host ("  {0}" -f ("-" * 95)) -ForegroundColor DarkGray

# Show prefetch impact
if ($r1.AvgSpeed -gt 0 -and $r2.AvgSpeed -gt 0) {
    $prefetchGain = (($r1.AvgSpeed - $r2.AvgSpeed) / $r2.AvgSpeed) * 100
    $sign = if ($prefetchGain -ge 0) { "+" } else { "" }
    Write-Host ""
    Write-Host "  Prefetch impact (Seafhttp vs Fileview): ${sign}$("{0:N1}" -f $prefetchGain)%" -ForegroundColor Cyan
}

if ($r2.AvgSpeed -gt 0 -and $r3.AvgSpeed -gt 0) {
    $shareOverhead = (($r3.AvgSpeed - $r2.AvgSpeed) / $r2.AvgSpeed) * 100
    $sign = if ($shareOverhead -ge 0) { "+" } else { "" }
    Write-Host "  Share link overhead vs Fileview:         ${sign}$("{0:N1}" -f $shareOverhead)%" -ForegroundColor Cyan
}

Write-Host ""
Write-Host "  Legend:" -ForegroundColor DarkGray
Write-Host "    prefetch  = fetch block N+1 while streaming block N" -ForegroundColor DarkGray
Write-Host "    batch     = batch Cassandra IN queries (100/batch) for block ID resolve" -ForegroundColor DarkGray
Write-Host "    4MB       = io.CopyBuffer with 4 MB sync.Pool buffers" -ForegroundColor DarkGray
Write-Host "    flush/4   = flush HTTP every 4 blocks instead of every block" -ForegroundColor DarkGray
Write-Host ""

# Cleanup
if (Test-Path $OutputDir) {
    Get-ChildItem "$OutputDir\bench-*.tmp" -ErrorAction SilentlyContinue | Remove-Item -Force
}

Write-Host "  Done!" -ForegroundColor Green
Write-Host ""
