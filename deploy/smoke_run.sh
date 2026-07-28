#!/usr/bin/env bash
# Workflow run smoke: healthz → login → POST /runs/trigger → poll run status.
set -euo pipefail

API_BASE="${API_BASE_URL:-http://localhost:8080}"
USERNAME="${ADMIN_USERNAME:-admin}"
PASSWORD="${ADMIN_PASSWORD:-admin123}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-120}"
POLL_TIMEOUT="${POLL_TIMEOUT:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-2}"

log() { printf '[smoke_run] %s\n' "$*"; }
die() { log "ERROR: $*"; exit 1; }

json_get() {
  local key="$1"
  if command -v jq >/dev/null 2>&1; then
    jq -r "$key"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "import json,sys; print(json.load(sys.stdin)$key)"
  else
    die "need jq or python3 to parse JSON"
  fi
}

wait_healthz() {
  log "waiting for ${API_BASE}/healthz (timeout ${HEALTH_TIMEOUT}s)..."
  local deadline=$((SECONDS + HEALTH_TIMEOUT))
  while (( SECONDS < deadline )); do
    if curl -sf "${API_BASE}/healthz" | json_get '["status"]' | grep -qx 'ok'; then
      log "healthz ok"
      return 0
    fi
    sleep 1
  done
  die "healthz not ready within ${HEALTH_TIMEOUT}s"
}

login() {
  log "logging in as ${USERNAME}..."
  local resp
  resp=$(curl -sf -X POST "${API_BASE}/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}") \
    || die "login request failed"
  TOKEN=$(printf '%s' "$resp" | json_get '["token"]')
  [[ -n "$TOKEN" && "$TOKEN" != "null" ]] || die "login response missing token"
  log "login ok"
}

trigger_run() {
  log "triggering workflow run..."
  local resp
  resp=$(curl -sf -X POST "${API_BASE}/api/v1/runs/trigger" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}') \
    || die "POST /runs/trigger failed"
  RUN_ID=$(printf '%s' "$resp" | json_get '["run_id"]')
  [[ -n "$RUN_ID" && "$RUN_ID" != "null" ]] || die "trigger response missing run_id"
  log "workflow run started run_id=${RUN_ID}"
}

poll_run() {
  log "polling run ${RUN_ID} (timeout ${POLL_TIMEOUT}s)..."
  local deadline=$((SECONDS + POLL_TIMEOUT)) status=""
  while (( SECONDS < deadline )); do
    status=$(curl -sf -H "Authorization: Bearer ${TOKEN}" \
      "${API_BASE}/api/v1/runs/${RUN_ID}" | json_get '["status"]') \
      || die "GET /runs/${RUN_ID} failed"
    case "$status" in
      executed|awaiting_approval|failed)
        log "run ${RUN_ID} status=${status}"
        [[ "$status" != "failed" ]] || exit 1
        return 0
        ;;
    esac
    sleep "$POLL_INTERVAL"
  done
  die "run ${RUN_ID} did not reach terminal status within ${POLL_TIMEOUT}s (last=${status})"
}

wait_healthz
login
trigger_run
poll_run
log "smoke passed"
exit 0
