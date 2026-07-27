# Alpaca Paper Authority — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation planning  
**Parent:** `2026-07-23-us-stock-paper-trading-agents-design.md` (P2 follow-up)  
**Related:** `2026-07-28-strategy-scheduler-runs-observability-design.md`

## 1. Goal

Make **Alpaca Paper** the system of record for market data (agents), account cash/equity, positions, and order execution. Keep Go API as the only component that talks to Alpaca Trading; keep agents proposal-only. Deliver display refresh via tiered polling first, then Go-proxied streaming (SSE).

### 1.1 Success criteria

- `agent-data` fetches daily OHLCV from Alpaca Market Data when `MARKET_DATA_PROVIDER=alpaca`.
- After risk gate (or `bypass_risk`), Go submits orders to Alpaca Paper; no local `ApplyFill` as authority.
- Overview / Portfolio / orders APIs read live state from Alpaca (with short TTL cache).
- Strategy `execution_mode` supports `require_approval`, `auto_reject_breaches`, and `bypass_risk`.
- Frontend: tiered REST polling by market session (phase 1); Go SSE proxy for quotes/trade updates (phase 2).
- Secrets stay server-side; browser never holds `ALPACA_*`.

### 1.2 Out of scope

- Live (non-paper) trading UI or default path
- Browser-direct Alpaca WebSocket
- Replacing agent daily bars with tick streams
- Multi-account / multi-tenant brokerage
- Options, crypto, margin strategies beyond what Alpaca Paper already allows

## 2. Product decisions (locked)

| Topic | Choice |
|-------|--------|
| Ledger authority | Alpaca Paper account (cash, positions, orders) |
| Local Postgres role | Runs, steps, proposals, approvals, strategies; optional order/account **mirrors** for audit — not fill authority |
| Delivery approach | **Phased:** phase 1 broker + REST display + polling; phase 2 SSE streams |
| Refresh (phase 1) | Tiered polling: positions/overview 15–30s open / 1–5m closed; orders 5–10s while open submissions exist; pause when tab hidden |
| Streaming (phase 2) | Go connects to Alpaca WS; forwards to browser via authenticated SSE |
| Risk gating | Strategy-driven via `execution_mode` (see §4) |
| Order type (v1) | Market orders; `client_order_id` ties to `trade_proposals.id` |
| Fill price | Alpaca fill only — stop using local EOD close as fill authority |
| Market data default | `MARKET_DATA_PROVIDER=alpaca`; `free` (Yahoo) remains fallback when keys missing |

## 3. Architecture

### 3.1 Trust boundaries

```text
Web (JWT)
  -> Go API
       -> Alpaca Trading REST (account, positions, orders)
       -> Alpaca Market Data WS / Trading trade_updates (phase 2) -> SSE to Web
  -> Go API -> Python agents (proposals only)

agent-data
  -> Alpaca Market Data REST (daily bars)

Agents never call Trading API.
Only Go places/cancels orders and reads account authority.
```

### 3.2 Component changes

| Component | Change |
|-----------|--------|
| Python `AlpacaMarketDataProvider` | Implement real bars fetch; read `ALPACA_API_KEY` / `ALPACA_API_SECRET` (+ data base URL) |
| Go `internal/broker` (new) | Thin Alpaca Paper client: account, positions, orders submit/get, optional cancel |
| Go workflow `Runner` | Replace `Ledger.ApplyFill` path with broker submit + status sync; risk path gated by `execution_mode` |
| Go overview/portfolio handlers | Source cash/positions/marks from broker (cache 5–10s) |
| Go approvals | On approve → broker submit (not local fill) |
| Local `ledger.ApplyFill` | Retire from production path; keep tests or mirror helpers only if needed for audit writes |
| Web Overview/Portfolio/Orders | Tiered polling hooks; phase 2 SSE client |
| Settings strategies | Add `bypass_risk` to `execution_mode` select |
| Deploy | Wire env; stop override forcing `MARKET_DATA_PROVIDER=free` when keys present |

### 3.3 Phased delivery

**Phase 1 — Authority + display polling**

1. Implement Alpaca market-data provider.
2. Implement Go broker client + config.
3. Workflow/approvals submit to Alpaca; sync fill state via REST (poll open orders until terminal or timeout).
4. Overview/Portfolio/orders read Alpaca.
5. Frontend tiered polling + settings `bypass_risk`.
6. Update flowchart/docs/README.

**Phase 2 — Streaming**

1. Go market + account stream hub (single upstream connection, fan-out to SSE clients).
2. `GET /api/v1/stream/market` and optional `GET /api/v1/stream/account` (JWT).
3. Frontend subscribe; REST remains reconciliation path.
4. `ALPACA_STREAM_ENABLED=true` to activate.

## 4. Strategy execution modes

| `execution_mode` | Go `risk.Evaluate` | Breach behavior | Submit to Alpaca |
|------------------|--------------------|-----------------|------------------|
| `require_approval` | Yes | Create approval; submit only after human approve | Yes when pass or approved |
| `auto_reject_breaches` | Yes | Reject proposal; no submit | Yes when pass |
| `bypass_risk` | **No** | N/A | Yes immediately for actionable proposals |

Seed default strategy remains `auto_reject_breaches`. Validation accepts the three modes above.

## 5. Execution flow

```text
[scheduler/manual] -> lock -> workflow_run
  -> agents (data from Alpaca bars) ...
  -> portfolio proposals persisted
  -> for each proposal:
       if mode != bypass_risk:
         evaluate Go risk (marks from Alpaca last quote or bar close)
         on breach: approval or reject per mode
       if allowed:
         POST Alpaca order (market)
         proposal status = submitted
         mirror order row (broker_order_id, client_order_id)
  -> poll/sync fills -> proposal filled|rejected|canceled
  -> run terminal status (executed | awaiting_approval | failed)
  -> optional NAV snapshot from Alpaca equity (audit)
```

### 5.1 Failure rules

- Missing Alpaca credentials: fail fast with clear error; run `failed`; UI shows config hint — no silent fake fills.
- Alpaca reject (e.g. buying power): proposal `rejected` with reason; other proposals in the run may still succeed.
- Rate limits: short backoff in Go; serve cached account/positions within TTL.
- Partial success across proposals remains allowed (same as today’s partial auto-exec).

### 5.2 Local ledger

- `ApplyFill` is **not** the source of truth after this change.
- Postgres may store mirror rows for orders linked to proposals for run detail UI.
- `INITIAL_CASH` no longer defines live cash; documentation states Paper account balance is authoritative. Seed/local cash may remain for offline tests with a mocked broker only.

## 6. Display, refresh, and streaming

### 6.1 API sources

| Endpoint / UI | Source |
|---------------|--------|
| Overview | Alpaca account + positions (+ recent runs from DB) |
| Portfolio | Alpaca positions |
| Orders (live) | Alpaca orders (+ join mirror for proposal/run) |
| Runs / Approvals / Strategies | Postgres unchanged |
| Quotes (phase 2) | SSE from Go proxy |

### 6.2 Phase 1 polling (frontend)

| Data | US market open | Closed / weekend |
|------|----------------|------------------|
| Overview / positions | 15–30s | 1–5 min or stop |
| Orders (while non-terminal local/broker opens) | 5–10s | same until terminal |
| Manual Refresh | always | always |

Pause timers when `document.hidden`. Use existing US/Eastern market clock helper to choose interval.

### 6.3 Phase 2 SSE

- Go holds Alpaca credentials; one upstream WS (or two: market data + trade_updates).
- Throttle per-symbol quote updates (e.g. min 1s) before fan-out.
- On trade_update, notify clients and/or invalidate account cache so next REST read is fresh.
- Reconnect with backoff on the browser; fall back to phase 1 polling if SSE unavailable.
- Without keys or with `ALPACA_STREAM_ENABLED=false`, stream endpoints return 503 with stable error body.

## 7. Configuration

| Variable | Default / notes |
|----------|-----------------|
| `ALPACA_API_KEY` | required for alpaca paths |
| `ALPACA_API_SECRET` | required for alpaca paths |
| `ALPACA_BASE_URL` | `https://paper-api.alpaca.markets` |
| `ALPACA_DATA_BASE_URL` | `https://data.alpaca.markets` |
| `MARKET_DATA_PROVIDER` | `alpaca` (fallback `free`) |
| `ALPACA_STREAM_ENABLED` | `false` until phase 2 ready |

Deploy templates and README must document Paper-only intent. Compose override must not force Yahoo when Alpaca is configured.

## 8. Testing

### Phase 1 (required)

- Python: Alpaca bars provider with HTTP mocked; factory selects provider from env.
- Go: broker client against `httptest.Server`; runner tests for all three `execution_mode` values; overview/portfolio handlers use broker mock, not ledger fills.
- Web: polling hook tests (open/closed/hidden); settings accepts `bypass_risk`.
- CI does not require real Alpaca credentials.

### Phase 2

- Stream hub unit tests with fake upstream; handler auth tests.
- Optional manual smoke with Paper keys.

## 9. Docs to update (with implementation)

- `docs/eod-workflow-flowchart.md` — ledger → Alpaca submit; modes include `bypass_risk`
- `README.md` / `deploy/README.md` — env and authority notes
- Parent V1 design: mark paper ledger authority superseded by this P2 spec for cash/positions/orders

## 10. Non-goals reminder

Yahoo/`free` remains a **dev fallback** only. Success of this project is measured by Paper authority + gated/bypass submit + truthful UI refresh — not by feature parity with a full brokerage terminal.
