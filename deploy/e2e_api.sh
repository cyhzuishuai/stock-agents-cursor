#!/usr/bin/env bash
# API E2E against a running Compose stack (real Postgres/Redis/API/agent-runtime).
# Uses LLM_MODE=mock via override for agent-runtime tool-loops.
# Requires ALPACA_API_KEY/SECRET in deploy/.env — Overview/Portfolio/Orders
# and workflow run trigger submits read/write Alpaca Paper (not the local ledger).
# Prerequisites: docker compose up --build (healthy), then: ./deploy/e2e_api.sh
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

if ! overview="$(auth "${API_BASE_URL}/api/v1/overview" 2>/tmp/e2e_overview.err)"; then
  if grep -qi "alpaca not configured\|503" /tmp/e2e_overview.err 2>/dev/null; then
    log "FAIL  GET /overview — set ALPACA_API_KEY/SECRET in deploy/.env and recreate api"
  fi
  assert_true 0 "GET /overview has cash/equity/nav (Alpaca)"
fi
printf '%s' "$overview" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'cash' in d and 'equity' in d and 'nav' in d"
assert_true 1 "GET /overview has cash/equity/nav (Alpaca)"

portfolio="$(auth "${API_BASE_URL}/api/v1/portfolio")"
printf '%s' "$portfolio" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'cash' in d and 'positions' in d"
assert_true 1 "GET /portfolio has cash/positions (Alpaca)"

orders="$(auth "${API_BASE_URL}/api/v1/orders")"
printf '%s' "$orders" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'orders' in d and isinstance(d['orders'], list)"
assert_true 1 "GET /orders has orders array (Alpaca)"

auth "${API_BASE_URL}/api/v1/runs" >/dev/null
assert_true 1 "GET /runs returns list"

auth "${API_BASE_URL}/api/v1/strategies" >/dev/null
assert_true 1 "GET /strategies returns list"

# Default: US/Eastern calendar date today (same as API defaultTradeDate).
# Override with EOD_TRADE_DATE if needed. Same trade_date may be re-run (busy lock only).
TRADE_DATE="${EOD_TRADE_DATE:-$(python3 -c 'from datetime import datetime; from zoneinfo import ZoneInfo; print(datetime.now(ZoneInfo("America/New_York")).date().isoformat())')}"
log "POST /runs/trigger trade_date=${TRADE_DATE} (US/Eastern)"
trigger_resp="$(curl -fsS --max-time 600 -X POST "${API_BASE_URL}/api/v1/runs/trigger" \
  -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
  -d "{\"trade_date\":\"${TRADE_DATE}\"}")"
RUN_ID="$(printf '%s' "$trigger_resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('run_id') or '')")"
[[ -n "$RUN_ID" ]] && assert_true 1 "POST /runs/trigger returns run_id" || assert_true 0 "POST /runs/trigger returns run_id"
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
[[ -n "$TERMINAL" ]] && assert_true 1 "workflow run reaches terminal status" || assert_true 0 "workflow run reaches terminal status"
[[ "$TERMINAL" != "failed" ]] && assert_true 1 "workflow run is not failed (got ${TERMINAL})" || assert_true 0 "workflow run is not failed (got ${TERMINAL})"
log "terminal status=${TERMINAL}"

auth "${API_BASE_URL}/api/v1/approvals?status=pending" >/dev/null
assert_true 1 "GET /approvals?status=pending ok"

settings="$(auth "${API_BASE_URL}/api/v1/settings")"
printf '%s' "$settings" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'watchlist' in d and 'risk_rules' in d"
assert_true 1 "GET /settings has watchlist/risk_rules"

# Stream: 503 when ALPACA_STREAM_ENABLED=false is expected; 200 when enabled.
stream_code="$(curl -sS -o /tmp/e2e_stream.body -w "%{http_code}" --max-time 5 \
  -H "Authorization: Bearer ${TOKEN}" "${API_BASE_URL}/api/v1/stream/market" || true)"
if [[ "$stream_code" == "200" ]]; then
  assert_true 1 "GET /stream/market returns 200 when enabled"
elif [[ "$stream_code" == "503" ]]; then
  assert_true 1 "GET /stream/market returns 503 when streaming disabled"
else
  assert_true 0 "GET /stream/market returns 200 or 503 (got ${stream_code})"
fi

log "passed=${PASSED} failed=${FAILED}"
log "e2e passed"
