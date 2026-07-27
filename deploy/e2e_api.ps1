# API E2E against a running Compose stack (real Postgres/Redis/API/agents).
# Uses LLM_MODE=mock via override — not production brokers or paid LLMs.
# Prerequisites: docker compose up (healthy), then:
#   powershell -File deploy/e2e_api.ps1
$ErrorActionPreference = "Stop"

$ApiBase = if ($env:API_BASE_URL) { $env:API_BASE_URL } else { "http://localhost:8080" }
$Username = if ($env:ADMIN_USERNAME) { $env:ADMIN_USERNAME } else { "admin" }
$Password = if ($env:ADMIN_PASSWORD) { $env:ADMIN_PASSWORD } else { "admin123" }
$HealthTimeoutSec = if ($env:HEALTH_TIMEOUT) { [int]$env:HEALTH_TIMEOUT } else { 120 }
$PollTimeoutSec = if ($env:POLL_TIMEOUT) { [int]$env:POLL_TIMEOUT } else { 300 }
$PollIntervalSec = if ($env:POLL_INTERVAL) { [int]$env:POLL_INTERVAL } else { 2 }

$script:Passed = 0
$script:Failed = 0

function Write-E2E([string]$Message) {
    Write-Host "[e2e_api] $Message"
}

function Assert-True([bool]$Condition, [string]$Name) {
    if ($Condition) {
        Write-E2E "PASS  $Name"
        $script:Passed++
    } else {
        Write-E2E "FAIL  $Name"
        $script:Failed++
        throw "assertion failed: $Name"
    }
}

function Wait-Healthz {
    Write-E2E "waiting for $ApiBase/healthz..."
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-RestMethod -Uri "$ApiBase/healthz" -Method Get -TimeoutSec 5
            if ($resp.status -eq "ok") {
                Assert-True $true "GET /healthz returns ok"
                return
            }
        } catch { }
        Start-Sleep -Seconds 1
    }
    throw "healthz not ready within ${HealthTimeoutSec}s"
}

function Get-AuthToken {
    $body = @{ username = $Username; password = $Password } | ConvertTo-Json
    $resp = Invoke-RestMethod -Uri "$ApiBase/api/v1/auth/login" -Method Post `
        -ContentType "application/json" -Body $body -TimeoutSec 30
    Assert-True ([bool]$resp.token) "POST /auth/login returns token"
    return [string]$resp.token
}

function Invoke-Authed([string]$Method, [string]$Path, [string]$Token, [string]$Body = "") {
    $headers = @{ Authorization = "Bearer $Token" }
    $uri = "$ApiBase$Path"
    if ($Body -ne "") {
        return Invoke-RestMethod -Uri $uri -Method $Method -Headers $headers `
            -ContentType "application/json" -Body $Body -TimeoutSec 600
    }
    return Invoke-RestMethod -Uri $uri -Method $Method -Headers $headers -TimeoutSec 30
}

try {
    Wait-Healthz
    $token = Get-AuthToken

    $overview = Invoke-Authed -Method Get -Path "/api/v1/overview" -Token $token
    Assert-True ($null -ne $overview.cash) "GET /overview has cash"
    Assert-True ($null -ne $overview.equity) "GET /overview has equity"

    $portfolio = Invoke-Authed -Method Get -Path "/api/v1/portfolio" -Token $token
    Assert-True ($null -ne $portfolio.cash) "GET /portfolio has cash"
    Assert-True ($null -ne $portfolio.positions) "GET /portfolio has positions"

    $runsBefore = Invoke-Authed -Method Get -Path "/api/v1/runs" -Token $token
    Assert-True ($null -ne $runsBefore) "GET /runs returns list"

    # Unique trade_date so re-runs hit a real EOD path (API rejects duplicate dates with 500).
    $tradeDate = if ($env:EOD_TRADE_DATE) {
        $env:EOD_TRADE_DATE
    } else {
        (Get-Date).AddDays(-(Get-Random -Minimum 3 -Maximum 40)).ToString("yyyy-MM-dd")
    }
    $eodBody = (@{ trade_date = $tradeDate } | ConvertTo-Json -Compress)
    Write-E2E "POST /runs/eod trade_date=$tradeDate"
    $eod = Invoke-Authed -Method Post -Path "/api/v1/runs/eod" -Token $token -Body $eodBody
    Assert-True ([bool]$eod.run_id) "POST /runs/eod returns run_id"
    $runId = [string]$eod.run_id
    Write-E2E "run_id=$runId"

    $deadline = (Get-Date).AddSeconds($PollTimeoutSec)
    $terminal = $null
    while ((Get-Date) -lt $deadline) {
        $run = Invoke-Authed -Method Get -Path "/api/v1/runs/$runId" -Token $token
        $st = [string]$run.status
        if ($st -in @("executed", "awaiting_approval", "failed")) {
            $terminal = $run
            break
        }
        Start-Sleep -Seconds $PollIntervalSec
    }
    Assert-True ($null -ne $terminal) "EOD run reaches terminal status"
    Assert-True ($terminal.status -ne "failed") "EOD run is not failed (got $($terminal.status))"
    Write-E2E "terminal status=$($terminal.status)"

    $approvals = Invoke-Authed -Method Get -Path "/api/v1/approvals?status=pending" -Token $token
    Assert-True ($null -ne $approvals) "GET /approvals?status=pending ok"

    $settings = Invoke-Authed -Method Get -Path "/api/v1/settings" -Token $token
    Assert-True ($null -ne $settings.watchlist) "GET /settings has watchlist"
    Assert-True ($null -ne $settings.risk_rules) "GET /settings has risk_rules"

    Write-E2E "passed=$script:Passed failed=$script:Failed"
    Write-E2E "e2e passed"
    exit 0
} catch {
    Write-E2E "ERROR: $($_.Exception.Message)"
    Write-E2E "passed=$script:Passed failed=$script:Failed"
    exit 1
}
