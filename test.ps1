#Requires -Version 5.1
<#
.SYNOPSIS
    Integration test for Everything Search Provider.

.DESCRIPTION
    Starts the HTTPS server, hits every endpoint, validates the responses,
    and prints a colour-coded pass/fail summary.

    Usage:  .\test.ps1
            .\test.ps1 -Query "notepad"
            .\test.ps1 -NoBuild
#>

param(
    [string]$Query = "",
    [switch]$NoBuild
)

$ErrorActionPreference = "Stop"

$passed = 0
$failed = 0

function Pass($msg)  { Write-Host "  [PASS] $msg" -ForegroundColor Green;  $script:passed += 1 }
function Fail($msg)  { Write-Host "  [FAIL] $msg" -ForegroundColor Red;    $script:failed += 1 }
function Info($msg)  { Write-Host "  [----] $msg" -ForegroundColor DarkGray }

# ── 1. Build (optional) ──────────────────────────────────────────────

Write-Host ""
Write-Host "=== Everything Search Provider — Integration Test ===" -ForegroundColor Cyan
Write-Host ""

if (-not $NoBuild) {
    Info "Building..."
    $outputDir = "Output"
    if (-not (Test-Path $outputDir)) { New-Item -ItemType Directory -Path $outputDir -Force | Out-Null }
    go build -o "Output\EverythingSearch.exe" .\Source 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Write-Host "Build failed." -ForegroundColor Red; exit 1 }
    Pass "Build succeeded"
}

# ── 2. Start the server ──────────────────────────────────────────────

Info "Starting server..."
Get-Process -Name "EverythingSearch" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 300


$proc = Start-Process -FilePath ".\Output\EverythingSearch.exe" `
    -RedirectStandardOutput "$env:TEMP\_es_stdout.log" `
    -RedirectStandardError  "$env:TEMP\_es_stderr.log" `
    -PassThru -WindowStyle Hidden

Start-Sleep -Seconds 3

if ($proc.HasExited) {
    $stderr = Get-Content "$env:TEMP\_es_stderr.log" -ErrorAction SilentlyContinue
    Fail "Server exited immediately: $stderr"
    Write-Host ""; exit 1
}
Pass "Server running (PID $($proc.Id))"

# ── 3. Helper to make requests ───────────────────────────────────────

# Ignore self-signed cert errors for this test session
if (-not ([System.Net.ServicePointManager]::ServerCertificateValidationCallback)) {
    [System.Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
}

function Invoke-Test {
    param(
        [string]$Method,
        [string]$Url,
        [hashtable]$ExpectedHeaders = @{}
    )
    try {
        $params = @{
            Uri                  = $Url
            Method               = $Method
            UseBasicParsing      = $true
            TimeoutSec           = 5
            SkipCertificateCheck = $true
        }
        $resp = Invoke-WebRequest @params
        return $resp
    } catch {
        return $null
    }
}

# ── 4. Test: OPTIONS preflight ───────────────────────────────────────

Info "Testing OPTIONS preflight..."
$resp = Invoke-Test -Method OPTIONS -Url "https://localhost:7654/api/search?qry=test"
if ($null -eq $resp) { Fail "OPTIONS returned no response" }
else {
    if ($resp.StatusCode -eq 200) { Pass "OPTIONS returns 200" } else { Fail "OPTIONS status: $($resp.StatusCode)" }

    $corsHeaders = @{
        "Access-Control-Allow-Origin"      = "https://www.bing.com"
        "Access-Control-Allow-Methods"     = "GET"
        "Access-Control-Allow-Credentials" = "true"
    }
    foreach ($kv in $corsHeaders.GetEnumerator()) {
        $actual = $resp.Headers[$kv.Key]
        if ($actual -and $actual[0] -eq $kv.Value) {
            Pass "Header $($kv.Key) = $($kv.Value)"
        } else {
            Fail "Header $($kv.Key): expected '$($kv.Value)', got '$($actual)'"
        }
    }
}

# ── 5. Test: Search and Rich Previews ───────────────────────────────────────

$testDir = Join-Path $PSScriptRoot "test_files"
$filesToTest = @()

if ($Query) {
    # If the user supplied a specific query, we test just that
    $filesToTest += [PSCustomObject]@{
        Name = $Query
        FullName = ""
        Extension = (([System.IO.Path]::GetExtension($Query)).ToLower())
    }
} else {
    if (-not (Test-Path $testDir)) {
        Fail "Directory 'test_files' not found. Please create a 'test_files' folder and populate it with example files (e.g., video, audio, text, and a binary file like .exe) to run the integrated tests."
    } else {
        $filesToTest = Get-ChildItem -Path $testDir -File
        if ($filesToTest.Count -eq 0) {
            Fail "Directory 'test_files' is empty. Please populate it with example files (e.g., video, audio, text, and a binary file like .exe) to run the integrated tests."
        }
    }
}

if ($filesToTest.Count -eq 0 -and $Query) {
    Info "No files to test."
} elseif ($filesToTest.Count -gt 0) {
    foreach ($f in $filesToTest) {
        $q = $f.Name
        Info "Testing GET /api/search?qry=$q ..."
        $searchResp = Invoke-Test -Method GET -Url "https://localhost:7654/api/search?qry=$q"

        if ($null -eq $searchResp) { Fail "Search returned no response for $($q)"; continue }
        if ($searchResp.StatusCode -eq 200) { Pass "Search returns 200 for $($q)" } else { Fail "Search status: $($searchResp.StatusCode) for $($q)" }

        try {
            $json = $searchResp.Content | ConvertFrom-Json
            if ($null -ne $json.Suggestions) { Pass "Suggestions property present for $($q)" } else { Fail "Missing Suggestions property in JSON for $($q)" }

            if ($json.Suggestions.Count -gt 0) {
                Pass "Results returned for $($q): $($json.Suggestions.Count) items"
            } else {
                Fail "Suggestions array is empty for $($q)"
            }
        } catch {
            Fail "Could not parse JSON for $($q): $($_.Exception.Message)"
        }

        # ── Preview Test ──
        $url = ""
        if ($f.FullName) {
            $encodedPath = [uri]::EscapeDataString($f.FullName)
            $url = "https://localhost:7654/api/preview?path=$encodedPath"
            Info "Testing GET /api/preview?path=... for $q"
        } else {
            $url = "https://localhost:7654/api/preview?path=$q"
            Info "Testing GET /api/preview?path=$q ..."
        }

        $previewResp = Invoke-Test -Method GET -Url $url
        if ($null -eq $previewResp) { Fail "Preview returned no response for $q"; continue }
        if ($previewResp.StatusCode -eq 200) { Pass "Preview returns 200 for $q" } else { Fail "Preview status: $($previewResp.StatusCode) for $q" }

        $html = $previewResp.Content
        if ($html -match "<!DOCTYPE html>") { Pass "Response is valid HTML for $q" } else { Fail "Response does not look like HTML for $q" }
        if ($html -match "Open File")       { Pass "Contains 'Open File' button for $q" }   else { Fail "Missing 'Open File' button for $q" }

        $ext = $f.Extension
        if (-not $ext) { $ext = "" }

        switch -Regex ($ext) {
            '^\.(jpg|jpeg|png|gif|bmp|webp|svg|ico)$' {
                if ($html -match '<img class="preview-media"') { Pass "Found <img class=`"preview-media`"> for $q" }
                else { Fail "Missing image tag for $q" }
            }
            '^\.(mp4|webm|ogg)$' {
                if ($html -match '<video class="preview-media"') { Pass "Found <video class=`"preview-media`"> for $q" }
                else { Fail "Missing video tag for $q" }
            }
            '^\.(mp3|wav|flac|m4a|aac)$' {
                if ($html -match '<audio class="preview-media"') { Pass "Found <audio class=`"preview-media`"> for $q" }
                else { Fail "Missing audio tag for $q" }
            }
            '^\.(txt|md|json|ini|bat|cmd|ps1|go|py|js|ts|c|cpp|h|cs|csv|log|yaml|yml|xml|css|html)$' {
                if ($html -match '<div class="text-preview">') { Pass "Found <div class=`"text-preview`"> for $q" }
                else { Fail "Missing text preview block for $q" }
            }
            default {
                if ($html -match '<div class="icon">') { Pass "Found fallback <div class=`"icon`"> for $q" }
                else { Fail "Missing icon block for $q" }
            }
        }
    }
}

# ── 8. Cleanup ───────────────────────────────────────────────────────

Info "Stopping server..."
$proc | Stop-Process -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\_es_stdout.log", "$env:TEMP\_es_stderr.log" -ErrorAction SilentlyContinue
Pass "Server stopped"

# ── Summary ──────────────────────────────────────────────────────────

Write-Host ""
$total = $passed + $failed
if ($failed -eq 0) {
    Write-Host "  All $total tests passed." -ForegroundColor Green
} else {
    Write-Host "  $passed/$total passed, $failed failed." -ForegroundColor Yellow
}
Write-Host ""
