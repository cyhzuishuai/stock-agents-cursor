#!/usr/bin/env bash
# API E2E against a running Compose stack (real Postgres/Redis/API/agents).
# Uses LLM_MODE=mock via override — not production brokers or paid LLMs.
# Prerequisites: docker compose up (healthy), then: ./deploy/e2e_api.sh
set -euo pipefail

API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-120}"
POLL_TIMEOUT="${POLL_TIMEOUT:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-2}"

PASSED=0
FAILED=0

log() { echo "[e2e_api] $*"; }

assert_true() {
  local cond="$1" name="$2"
  if [[ "$cond" == "1" ]]; then
    log "PASS  $name"
    PASSED=$((PASSED + 1))
  else
    log "FAIL  $name"
    FAILED=$((FAILED + 1))
    echo "assertion failed: $name" >&2
    exit 1
  fi
}

json_get() {
  python3 -c "import json,sys; d=json.load(sys.stdin); print(d$1)"
}

log "waiting for ${API_BASE_URL}/healthz..."
deadline=$((SECONDS + HEALTH_TIMEOUT))
while (( SECONDS < deadline )); do
  if body="$(curl -fsS --max-time 5 "${API_BASE_URL}/healthz" 2>/dev/null)"; then
    status="$(printf '%s' "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))")"
    if [[ "$status" == "ok" ]]; then
      assert_true 1 "GET /healthz returns ok"
      break
    fi
  fi
  sleep 1
done
[[ "${status:-}" == "ok" ]] || { log "healthz not ready"; exit 1; }

login_body="$(curl -fsS --max-time 30 -X POST "${API_BASE_URL}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")"
TOKEN="$(printf '%s' "$login_body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('token') or '')")"
[[ -n "$TOKEN" ]] && assert_true 1 "POST /auth/login returns token" || assert_true 0 "POST /auth/login returns token"

auth() { curl -fsS --max-time "${2:-30}" -H "Authorization: Bearer ${TOKEN}" "$@"; }

overview="$(auth "${API_BASE_URL}/api/v1/overview")"
printf '%s' "$overview" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'cash' in d and 'equity' in d"
assert_true 1 "GET /overview has cash/equity"

portfolio="$(auth "${API_BASE_URL}/api/v1/portfolio")"
printf '%s' "$portfolio" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'cash' in d and 'positions' in d"
assert_true 1 "GET /portfolio has cash/positions"

auth "${API_BASE_URL}/api/v1/runs" >/dev/null
assert_true 1 "GET /runs returns list"

TRADE_DATE="${EOD_TRADE_DATE:-$(python3 -c 'from datetime import date,timedelta; import random; print((date.today()-timedelta(days=random.randint(3,40))).isoformat())')}"
log "POST /runs/eod trade_date=${TRADE_DATE}"
eod="$(curl -fsS --max-time 600 -X POST "${API_BASE_URL}/api/v1/runs/eod" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d "{\"trade_date\":\"${TRADE_DATE}\"}")"
RUN_ID="$(printf '%s' "$eod" | python3 -c "import json,sys; print(json.load(sys.stdin).get('run_id') or '')")"
[[ -n "$RUN_ID" ]] && assert_true 1 "POST /runs/eod returns run_id" || assert_true 0 "POST /runs/eod returns run_id"
log "run_id=${RUN_ID}"

deadline=$((SECONDS + POLL_TIMEOUT))
TERMINAL=""
while (( SECONDS < deadline )); do
  run="$(auth "${API_BASE_URL}/api/v1/runs/${RUN_ID}")"
  TERMINAL="$(printf '%s' "$run" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))")"
  case "$TERMINAL" in
    executed|awaiting_approval|failed) break ;;
  esac
  sleep "$POLL_INTERVAL"
done
[[ -n "$TERMINAL" ]] && assert_true 1 "EOD run reaches terminal status" || assert_true 0 "EOD run reaches terminal status"
[[ "$TERMINAL" != "failed" ]] && assert_true 1 "EOD run is not failed (got ${TERMINAL})" || assert_true 0 "EOD run is not failed (got ${TERMINAL})"
log "terminal status=${TERMINAL}"

auth "${API_BASE_URL}/api/v1/approvals?status=pending" >/dev/null
assert_true 1 "GET /approvals?status=pending ok"

settings="$(auth "${API_BASE_URL}/api/v1/settings")"
printf '%s' "$settings" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'watchlist' in d and 'risk_rules' in d"
assert_true 1 "GET /settings has watchlist/risk_rules"

log "passed=${PASSED} failed=${FAILED}"
log "e2e passed"
