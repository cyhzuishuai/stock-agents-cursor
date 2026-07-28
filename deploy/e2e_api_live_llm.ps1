# Live-LLM API E2E: strategy workflow trigger with real MiniMax (or other OpenAI-compatible LLM).
# Prerequisites:
#   - deploy/.env: LLM_MODE=live, LLM_API_KEY, LLM_BASE_URL, LLM_MODEL=MiniMax-M3, ALPACA_* 
#   - docker compose up --build (agent-runtime NOT forced to mock)
#   powershell -File deploy/e2e_api_live_llm.ps1
$ErrorActionPreference = "Stop"

$ApiBase = if ($env:API_BASE_URL) { $env:API_BASE_URL } else { "http://localhost:8080" }
$Username = if ($env:ADMIN_USERNAME) { $env:ADMIN_USERNAME } else { "admin" }
$Password = if ($env:ADMIN_PASSWORD) { $env:ADMIN_PASSWORD } else { "admin123" }
$HealthTimeoutSec = if ($env:HEALTH_TIMEOUT) { [int]$env:HEALTH_TIMEOUT } else { 180 }
$PollTimeoutSec = if ($env:POLL_TIMEOUT) { [int]$env:POLL_TIMEOUT } else { 900 }
$PollIntervalSec = if ($env:POLL_INTERVAL) { [int]$env:POLL_INTERVAL } else { 5 }

$script:Passed = 0
$script:Failed = 0

function Write-E2E([string]$Message) {
    Write-Host "[e2e_live_llm] $Message"
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
        Start-Sleep -Seconds 2
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
            -ContentType "application/json" -Body $Body -TimeoutSec 900
    }
    return Invoke-RestMethod -Uri $uri -Method $Method -Headers $headers -TimeoutSec 60
}

function Get-StepPayload([object]$RunDetail, [string]$StepName) {
    foreach ($s in $RunDetail.steps) {
        if ([string]$s.step -eq $StepName) {
            return $s
        }
    }
    return $null
}

function Assert-EnvelopeStep([object]$Step, [string]$Name) {
    Assert-True ($null -ne $Step) "run has step $Name"
    Assert-True ([string]$Step.status -eq "ok") "step $Name status is ok (got $($Step.status))"
    $raw = [string]$Step.payload_json
    Assert-True ($raw.Length -gt 2) "step $Name has payload_json"
    $payload = $raw | ConvertFrom-Json
    Assert-True ($null -ne $payload.result) "step $Name payload has result"
    Assert-True ($null -ne $payload.trace) "step $Name payload has trace"
    $rounds = @($payload.trace.rounds)
    Assert-True ($rounds.Count -ge 1) "step $Name trace.rounds length >= 1 (got $($rounds.Count))"
    Write-E2E "step $Name stop_reason=$($payload.trace.stop_reason) rounds=$($rounds.Count)"
}

try {
    Wait-Healthz

    try {
        $rt = Invoke-RestMethod -Uri "http://localhost:8001/healthz" -TimeoutSec 5
        Assert-True ($rt.status -eq "ok") "agent-runtime /healthz ok"
    } catch {
        Write-E2E "WARN: could not reach agent-runtime:8001 healthz ($($_.Exception.Message))"
    }

    $token = Get-AuthToken

    $tradeDate = if ($env:RUN_TRADE_DATE) {
        $env:RUN_TRADE_DATE
    } else {
        [TimeZoneInfo]::ConvertTimeBySystemTimeZoneId(
            (Get-Date),
            "Eastern Standard Time"
        ).ToString("yyyy-MM-dd")
    }
    $triggerBody = (@{ trade_date = $tradeDate } | ConvertTo-Json -Compress)
    Write-E2E "POST /runs/trigger trade_date=$tradeDate (live LLM; may take several minutes)"
    $triggerResp = Invoke-Authed -Method Post -Path "/api/v1/runs/trigger" -Token $token -Body $triggerBody
    Assert-True ([bool]$triggerResp.run_id) "POST /runs/trigger returns run_id"
    $runId = [string]$triggerResp.run_id
    Write-E2E "run_id=$runId"

    $deadline = (Get-Date).AddSeconds($PollTimeoutSec)
    $terminal = $null
    while ((Get-Date) -lt $deadline) {
        $run = Invoke-Authed -Method Get -Path "/api/v1/runs/$runId" -Token $token
        $st = [string]$run.status
        Write-E2E "poll status=$st"
        if ($st -in @("executed", "awaiting_approval", "failed")) {
            $terminal = $run
            break
        }
        Start-Sleep -Seconds $PollIntervalSec
    }
    Assert-True ($null -ne $terminal) "workflow run reaches terminal status"
    Assert-True ($terminal.status -ne "failed") "workflow run is not failed (got $($terminal.status))"
    Write-E2E "terminal status=$($terminal.status)"

    $detail = Invoke-Authed -Method Get -Path "/api/v1/runs/$runId" -Token $token
    $analyst = Get-StepPayload -RunDetail $detail -StepName "analyst"
    $portfolio = Get-StepPayload -RunDetail $detail -StepName "portfolio"
    Assert-EnvelopeStep -Step $analyst -Name "analyst"
    Assert-EnvelopeStep -Step $portfolio -Name "portfolio"

    Write-E2E "passed=$script:Passed failed=$script:Failed"
    Write-E2E "live llm e2e passed"
    exit 0
} catch {
    Write-E2E "ERROR: $($_.Exception.Message)"
    if ($_.ErrorDetails.Message) { Write-E2E "DETAIL: $($_.ErrorDetails.Message)" }
    Write-E2E "passed=$script:Passed failed=$script:Failed"
    exit 1
}
