# API DTO sketch (V1)

## Auth
POST /api/v1/auth/login { "username", "password" } -> { "token" }
GET  /api/v1/auth/me -> { "id", "username" }

## Overview
GET /api/v1/overview -> {
  cash, equity, nav, pending_approvals_count,
  latest_run: { id, trade_date, status },
  positions_summary: [{ symbol, qty, market_value, weight }],
  nav_series: [{ trade_date, nav }]
}

## Portfolio
GET /api/v1/portfolio -> { cash, positions: [{ symbol, qty, avg_cost, stop_loss, take_profit, market_price, unrealized_pnl, weight }] }

## Runs
GET /api/v1/runs -> [{ id, trade_date, status, created_at }]
GET /api/v1/runs/:id -> { id, trade_date, status, steps: [...], proposals: [...], orders: [...] }
POST /api/v1/runs/eod { "trade_date"?: "YYYY-MM-DD" } -> { run_id }
POST /api/v1/runs/:id/cancel -> { ok: true }

## Approvals
GET /api/v1/approvals?status=pending -> [{ id, proposal_id, symbol, side, qty, breach_reasons, created_at }]
POST /api/v1/approvals/:id/decide { "decision": "approved"|"rejected", "note"?: string } -> { ok: true }

## Settings
GET /api/v1/settings -> {
  watchlist: [{ symbol, can_hold }],
  risk_rules: object,
  market_data_provider: string
}
POST /api/v1/settings/watchlist { symbol, can_hold? } -> { symbol, can_hold }
PATCH /api/v1/settings/watchlist/:symbol { can_hold } -> { symbol, can_hold }
DELETE /api/v1/settings/watchlist/:symbol -> { ok: true }
PATCH /api/v1/settings/risk/:key { value: number } -> { key, value }
GET /api/v1/symbols/search?q= -> [{ symbol, name }]

## Internal
POST /internal/eod/run  Header X-Internal-Token -> { run_id }
