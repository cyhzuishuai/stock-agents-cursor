# API E2E against a running Compose stack (real Postgres/Redis/API/agent-runtime).
# Uses LLM_MODE=mock via override for agent-runtime tool-loops.
# Requires ALPACA_API_KEY/SECRET in deploy/.env — Overview/Portfolio/Orders
# and workflow run trigger submits read/write Alpaca Paper (not the local ledger).
# Prerequisites: docker compose up --build (healthy), then:
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

    try {
        $overview = Invoke-Authed -Method Get -Path "/api/v1/overview" -Token $token
    } catch {
        $msg = $_.Exception.Message
        if ($msg -match "503|alpaca not configured") {
            throw "GET /overview returned alpaca not configured — set ALPACA_API_KEY/SECRET in deploy/.env and recreate api"
        }
        throw
    }
    Assert-True ($null -ne $overview.cash) "GET /overview has cash (Alpaca)"
    Assert-True ($null -ne $overview.equity) "GET /overview has equity (Alpaca)"
    Assert-True ($null -ne $overview.nav) "GET /overview has nav (Alpaca)"

    $portfolio = Invoke-Authed -Method Get -Path "/api/v1/portfolio" -Token $token
    Assert-True ($null -ne $portfolio.cash) "GET /portfolio has cash (Alpaca)"
    Assert-True ($null -ne $portfolio.positions) "GET /portfolio has positions (Alpaca)"

    $orders = Invoke-Authed -Method Get -Path "/api/v1/orders" -Token $token
    Assert-True ($null -ne $orders.orders) "GET /orders has orders array (Alpaca)"

    $runsBefore = Invoke-Authed -Method Get -Path "/api/v1/runs" -Token $token
    Assert-True ($null -ne $runsBefore) "GET /runs returns list"

    $strategies = Invoke-Authed -Method Get -Path "/api/v1/strategies" -Token $token
    Assert-True ($null -ne $strategies) "GET /strategies returns list"

    # Default: US/Eastern calendar date today (same as API defaultTradeDate).
    # Override with EOD_TRADE_DATE if needed. Same trade_date may be re-run (busy lock only).
    $tradeDate = if ($env:EOD_TRADE_DATE) {
        $env:EOD_TRADE_DATE
    } else {
        [TimeZoneInfo]::ConvertTimeBySystemTimeZoneId(
            (Get-Date),
            "Eastern Standard Time"
        ).ToString("yyyy-MM-dd")
    }
    $triggerBody = (@{ trade_date = $tradeDate } | ConvertTo-Json -Compress)
    Write-E2E "POST /runs/trigger trade_date=$tradeDate (US/Eastern)"
    $triggerResp = Invoke-Authed -Method Post -Path "/api/v1/runs/trigger" -Token $token -Body $triggerBody
    Assert-True ([bool]$triggerResp.run_id) "POST /runs/trigger returns run_id"
    $runId = [string]$triggerResp.run_id
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
    Assert-True ($null -ne $terminal) "workflow run reaches terminal status"
    Assert-True ($terminal.status -ne "failed") "workflow run is not failed (got $($terminal.status))"
    Write-E2E "terminal status=$($terminal.status)"

    $approvals = Invoke-Authed -Method Get -Path "/api/v1/approvals?status=pending" -Token $token
    Assert-True ($null -ne $approvals) "GET /approvals?status=pending ok"

    $settings = Invoke-Authed -Method Get -Path "/api/v1/settings" -Token $token
    Assert-True ($null -ne $settings.watchlist) "GET /settings has watchlist"
    Assert-True ($null -ne $settings.risk_rules) "GET /settings has risk_rules"

    # Stream: 503 when ALPACA_STREAM_ENABLED=false is OK; 401 without token already covered by middleware.
    try {
        $headers = @{ Authorization = "Bearer $token" }
        $streamResp = Invoke-WebRequest -Uri "$ApiBase/api/v1/stream/market" -Headers $headers `
            -Method Get -TimeoutSec 5 -ErrorAction Stop
        Assert-True ($streamResp.StatusCode -eq 200) "GET /stream/market returns 200 when enabled"
    } catch {
        $code = $null
        if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
        Assert-True ($code -eq 503) "GET /stream/market returns 503 when streaming disabled (got $code)"
    }

    Write-E2E "passed=$script:Passed failed=$script:Failed"
    Write-E2E "e2e passed"
    exit 0
} catch {
    Write-E2E "ERROR: $($_.Exception.Message)"
    Write-E2E "passed=$script:Passed failed=$script:Failed"
    exit 1
}
