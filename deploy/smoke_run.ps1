# Workflow run smoke: healthz → login → POST /runs/trigger → poll run status.
$ErrorActionPreference = "Stop"

$ApiBase = if ($env:API_BASE_URL) { $env:API_BASE_URL } else { "http://localhost:8080" }
$Username = if ($env:ADMIN_USERNAME) { $env:ADMIN_USERNAME } else { "admin" }
$Password = if ($env:ADMIN_PASSWORD) { $env:ADMIN_PASSWORD } else { "admin123" }
$HealthTimeoutSec = if ($env:HEALTH_TIMEOUT) { [int]$env:HEALTH_TIMEOUT } else { 120 }
$PollTimeoutSec = if ($env:POLL_TIMEOUT) { [int]$env:POLL_TIMEOUT } else { 300 }
$PollIntervalSec = if ($env:POLL_INTERVAL) { [int]$env:POLL_INTERVAL } else { 2 }

function Write-SmokeLog([string]$Message) {
    Write-Host "[smoke_run] $Message"
}

function Wait-Healthz {
    Write-SmokeLog "waiting for $ApiBase/healthz (timeout ${HealthTimeoutSec}s)..."
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSec)
    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-RestMethod -Uri "$ApiBase/healthz" -Method Get -TimeoutSec 5
            if ($resp.status -eq "ok") {
                Write-SmokeLog "healthz ok"
                return
            }
        } catch {
            # retry until timeout
        }
        Start-Sleep -Seconds 1
    }
    throw "healthz not ready within ${HealthTimeoutSec}s"
}

function Get-AuthToken {
    Write-SmokeLog "logging in as $Username..."
    $body = @{ username = $Username; password = $Password } | ConvertTo-Json
    $resp = Invoke-RestMethod -Uri "$ApiBase/api/v1/auth/login" -Method Post `
        -ContentType "application/json" -Body $body -TimeoutSec 30
    if (-not $resp.token) { throw "login response missing token" }
    Write-SmokeLog "login ok"
    return $resp.token
}

function Start-RunTrigger([string]$Token) {
    Write-SmokeLog "triggering workflow run..."
    $headers = @{ Authorization = "Bearer $Token" }
    $resp = Invoke-RestMethod -Uri "$ApiBase/api/v1/runs/trigger" -Method Post `
        -Headers $headers -ContentType "application/json" -Body "{}" -TimeoutSec 600
    if (-not $resp.run_id) { throw "trigger response missing run_id" }
    Write-SmokeLog "workflow run started run_id=$($resp.run_id)"
    return [string]$resp.run_id
}

function Wait-RunTerminal([string]$Token, [string]$RunId) {
    Write-SmokeLog "polling run $RunId (timeout ${PollTimeoutSec}s)..."
    $headers = @{ Authorization = "Bearer $Token" }
    $deadline = (Get-Date).AddSeconds($PollTimeoutSec)
    $lastStatus = ""
    while ((Get-Date) -lt $deadline) {
        $run = Invoke-RestMethod -Uri "$ApiBase/api/v1/runs/$RunId" -Method Get `
            -Headers $headers -TimeoutSec 30
        $lastStatus = [string]$run.status
        switch ($lastStatus) {
            "executed" { Write-SmokeLog "run $RunId status=executed"; return }
            "awaiting_approval" { Write-SmokeLog "run $RunId status=awaiting_approval"; return }
            "failed" {
                Write-SmokeLog "run $RunId status=failed"
                exit 1
            }
        }
        Start-Sleep -Seconds $PollIntervalSec
    }
    throw "run $RunId did not reach terminal status within ${PollTimeoutSec}s (last=$lastStatus)"
}

try {
    Wait-Healthz
    $token = Get-AuthToken
    $runId = Start-RunTrigger -Token $token
    Wait-RunTerminal -Token $token -RunId $runId
    Write-SmokeLog "smoke passed"
    exit 0
} catch {
    Write-SmokeLog "ERROR: $($_.Exception.Message)"
    exit 1
}
