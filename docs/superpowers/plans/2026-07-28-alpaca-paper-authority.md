# Alpaca Paper Authority Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Alpaca Paper the authority for market bars, cash/positions/orders, and order submission; keep Go risk/approval gates (plus `bypass_risk`); refresh the UI via tiered polling (phase 1) then Go-proxied SSE (phase 2).

**Architecture:** Python `AlpacaMarketDataProvider` fetches daily bars. New Go `internal/broker` client talks to Alpaca Paper Trading REST. Workflow/approvals submit market orders instead of `ledger.ApplyFill`. Overview/Portfolio read Alpaca via a TTL cache. Web adds session-aware polling; phase 2 adds SSE fan-out from Alpaca websockets.

**Tech Stack:** Python 3.12 + httpx + pytest; Go 1.22+ (Gin, Gorm, net/http); Next.js App Router + Vitest; Alpaca Paper + Market Data APIs.

**Spec:** `docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`

## Global Constraints

- Paper trading only (`ALPACA_BASE_URL` defaults to `https://paper-api.alpaca.markets`)
- Agents never call Trading API; only Go holds trading credentials for orders/account
- `execution_mode`: `require_approval` | `auto_reject_breaches` | `bypass_risk`
- Local `ApplyFill` is not production authority after Task 4
- CI must not require real Alpaca keys (use `httptest.Server` / httpx mock)
- Do not commit unless the user explicitly asks (Commit steps are optional gates)
- Run relevant Go tests / Python pytest / `cd apps/web && npm test` before claiming a task done
- Phase 2 (Tasks 9–11) starts only after Phase 1 Tasks 1–8 are green

---

## File map

| File | Responsibility |
|------|----------------|
| `services/agents/common/stock_agents_common/marketdata/alpaca.py` | Real Alpaca bars provider |
| `services/agents/common/tests/test_marketdata_alpaca.py` | Mocked HTTP tests for Alpaca bars |
| `services/api/internal/config/config.go` | `ALPACA_*`, stream flag |
| `services/api/internal/broker/types.go` | Account/Position/Order DTOs + `Client` interface |
| `services/api/internal/broker/alpaca.go` | REST client |
| `services/api/internal/broker/cache.go` | 5–10s TTL cache for account/positions |
| `services/api/internal/broker/alpaca_test.go` | httptest tests |
| `services/api/internal/models/order.go` | Mirror fields: `BrokerOrderID`, `ClientOrderID`, `ProposalID` |
| `services/api/internal/models/proposal.go` | Status includes `submitted` |
| `services/api/internal/workflow/steps.go` | `bypass_risk`, `ProposalSubmitted` |
| `services/api/internal/workflow/runner.go` | Broker submit + sync; skip risk when bypass |
| `services/api/internal/approvals/service.go` | Approve → broker submit |
| `services/api/internal/httpserver/handlers_overview.go` | Read broker account/positions |
| `services/api/internal/httpserver/handlers_portfolio.go` | Read broker positions |
| `services/api/internal/httpserver/handlers_orders.go` | List Alpaca orders (+ mirror join) |
| `services/api/internal/httpserver/handlers_stream.go` | Phase 2 SSE |
| `services/api/internal/stream/hub.go` | Phase 2 upstream WS fan-out |
| `apps/web/src/lib/useTieredPolling.ts` | Session-aware poll hook |
| `apps/web/src/app/(shell)/page.tsx` | Overview polling |
| `apps/web/src/app/(shell)/portfolio/page.tsx` | Portfolio polling |
| `apps/web/src/app/(shell)/settings/page.tsx` | `bypass_risk` option |
| `deploy/env.example`, `deploy/docker-compose*.yml`, READMEs, flowchart | Config + docs |

---

### Task 1: Alpaca market-data provider (Python)

**Files:**
- Modify: `services/agents/common/stock_agents_common/marketdata/alpaca.py`
- Create: `services/agents/common/tests/test_marketdata_alpaca.py`
- Modify: `services/agents/common/tests/test_marketdata_free.py` (remove stub expectation; keep factory type check)
- Modify: `deploy/env.example` — default `MARKET_DATA_PROVIDER=alpaca`; add `ALPACA_DATA_BASE_URL=`
- Modify: `deploy/docker-compose.yml` — default `${MARKET_DATA_PROVIDER:-alpaca}`
- Modify: `deploy/docker-compose.override.yml` — remove hard `MARKET_DATA_PROVIDER: free` (use env / leave unset so `.env` wins)

**Interfaces:**
- Produces: `AlpacaMarketDataProvider.get_daily_bars(symbols, trade_date) -> list[dict]` with keys `symbol,trade_date,open,high,low,close,volume` (same as free provider)
- Consumes: env `ALPACA_API_KEY`, `ALPACA_API_SECRET`, optional `ALPACA_DATA_BASE_URL` (default `https://data.alpaca.markets`)

- [ ] **Step 1: Write failing tests**

```python
# services/agents/common/tests/test_marketdata_alpaca.py
from __future__ import annotations

import httpx
import pytest

from stock_agents_common.marketdata.alpaca import AlpacaMarketDataProvider


def test_get_daily_bars_maps_alpaca_bars(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ALPACA_API_KEY", "k")
    monkeypatch.setenv("ALPACA_API_SECRET", "s")

    payload = {
        "bars": {
            "AAPL": [
                {
                    "t": "2026-07-22T04:00:00Z",
                    "o": 100.0,
                    "h": 110.0,
                    "l": 99.0,
                    "c": 105.0,
                    "v": 1_000_000,
                }
            ]
        }
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers.get("APCA-API-KEY-ID") == "k"
        assert request.headers.get("APCA-API-SECRET-KEY") == "s"
        assert "/v2/stocks/bars" in str(request.url)
        return httpx.Response(200, json=payload)

    transport = httpx.MockTransport(handler)
    client = httpx.Client(transport=transport)
    provider = AlpacaMarketDataProvider(client=client)
    bars = provider.get_daily_bars(["AAPL"], "2026-07-22")
    assert bars == [
        {
            "symbol": "AAPL",
            "trade_date": "2026-07-22",
            "open": 100.0,
            "high": 110.0,
            "low": 99.0,
            "close": 105.0,
            "volume": 1_000_000.0,
        }
    ]


def test_get_daily_bars_requires_keys(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("ALPACA_API_KEY", raising=False)
    monkeypatch.delenv("ALPACA_API_SECRET", raising=False)
    provider = AlpacaMarketDataProvider(client=httpx.Client(transport=httpx.MockTransport(lambda r: httpx.Response(500))))
    with pytest.raises(ValueError, match="ALPACA_API_KEY"):
        provider.get_daily_bars(["AAPL"], "2026-07-22")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/agents/common && python -m pytest tests/test_marketdata_alpaca.py -v`  
Expected: FAIL (import/NotImplemented or missing behavior)

- [ ] **Step 3: Implement provider**

```python
# services/agents/common/stock_agents_common/marketdata/alpaca.py
"""Alpaca Market Data daily bars provider."""

from __future__ import annotations

import os
from datetime import datetime, timedelta, timezone

import httpx


class AlpacaMarketDataProvider:
    def __init__(
        self,
        client: httpx.Client | None = None,
        *,
        api_key: str | None = None,
        api_secret: str | None = None,
        base_url: str | None = None,
    ) -> None:
        self._client = client
        self._owns_client = client is None
        self._api_key = api_key if api_key is not None else os.getenv("ALPACA_API_KEY", "")
        self._api_secret = api_secret if api_secret is not None else os.getenv("ALPACA_API_SECRET", "")
        self._base_url = (
            base_url
            or os.getenv("ALPACA_DATA_BASE_URL")
            or "https://data.alpaca.markets"
        ).rstrip("/")

    def close(self) -> None:
        if self._owns_client and self._client is not None:
            self._client.close()
            self._client = None

    def _client_or_default(self) -> httpx.Client:
        if self._client is None:
            self._client = httpx.Client(timeout=30.0)
        return self._client

    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        if not self._api_key or not self._api_secret:
            raise ValueError("ALPACA_API_KEY and ALPACA_API_SECRET are required")
        day = datetime.strptime(trade_date, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        start = day.isoformat().replace("+00:00", "Z")
        end = (day + timedelta(days=1)).isoformat().replace("+00:00", "Z")
        client = self._client_or_default()
        resp = client.get(
            f"{self._base_url}/v2/stocks/bars",
            params={
                "symbols": ",".join(symbols),
                "timeframe": "1Day",
                "start": start,
                "end": end,
                "adjustment": "raw",
                "feed": "iex",
            },
            headers={
                "APCA-API-KEY-ID": self._api_key,
                "APCA-API-SECRET-KEY": self._api_secret,
            },
        )
        resp.raise_for_status()
        raw_bars = (resp.json() or {}).get("bars") or {}
        out: list[dict] = []
        for symbol in symbols:
            rows = raw_bars.get(symbol) or []
            if not rows:
                continue
            row = rows[0]
            out.append(
                {
                    "symbol": symbol,
                    "trade_date": trade_date,
                    "open": float(row["o"]),
                    "high": float(row["h"]),
                    "low": float(row["l"]),
                    "close": float(row["c"]),
                    "volume": float(row["v"]),
                }
            )
        return out
```

Update `test_get_provider_alpaca_stub_raises` → rename to assert provider type only (no NotImplemented).

- [ ] **Step 4: Run tests**

Run: `cd services/agents/common && python -m pytest tests/test_marketdata_alpaca.py tests/test_marketdata_free.py -v`  
Expected: PASS

- [ ] **Step 5: Commit (optional)**

```bash
git add services/agents/common deploy/env.example deploy/docker-compose.yml deploy/docker-compose.override.yml
git commit -m "feat: implement Alpaca market data bars provider"
```

---

### Task 2: Go config + broker client

**Files:**
- Modify: `services/api/internal/config/config.go`
- Modify: `services/api/internal/config/config_test.go`
- Create: `services/api/internal/broker/types.go`
- Create: `services/api/internal/broker/alpaca.go`
- Create: `services/api/internal/broker/cache.go`
- Create: `services/api/internal/broker/alpaca_test.go`
- Modify: `services/api/cmd/api/main.go` — construct broker client, attach to API/Runner

**Interfaces:**
- Produces:

```go
// package broker
type Account struct {
    ID               string
    Cash             float64
    Equity           float64
    BuyingPower      float64
    PortfolioValue   float64
}

type Position struct {
    Symbol        string
    Qty           float64
    AvgCost       float64
    MarketValue   float64
    CurrentPrice  float64
    UnrealizedPL  float64
}

type OrderRequest struct {
    Symbol        string
    Side          string // buy|sell
    Qty           float64
    ClientOrderID string
    TimeInForce   string // day
    Type          string // market
}

type Order struct {
    ID            string
    ClientOrderID string
    Symbol        string
    Side          string
    Qty           float64
    FilledQty     float64
    FilledAvgPrice float64
    Status        string // new|accepted|partially_filled|filled|canceled|rejected|...
}

type Client interface {
    GetAccount(ctx context.Context) (Account, error)
    ListPositions(ctx context.Context) ([]Position, error)
    SubmitOrder(ctx context.Context, req OrderRequest) (Order, error)
    GetOrder(ctx context.Context, brokerOrderID string) (Order, error)
    ListOrders(ctx context.Context, status string) ([]Order, error) // status e.g. "open"|"closed"|"all"
}
```

- Produces: `NewCachedClient(inner Client, ttl time.Duration) Client` caching only `GetAccount` + `ListPositions` (invalidate on successful `SubmitOrder`)
- Config fields: `AlpacaAPIKey`, `AlpacaAPISecret`, `AlpacaBaseURL`, `AlpacaStreamEnabled bool`

- [ ] **Step 1: Extend config with failing test**

```go
// In config_test.go — set env ALPACA_API_KEY=k ALPACA_API_SECRET=s ALPACA_BASE_URL=https://paper-api.alpaca.markets
// Load() and assert cfg.AlpacaAPIKey == "k", cfg.AlpacaBaseURL default when unset is paper URL
```

Defaults when unset:
- `AlpacaBaseURL` = `https://paper-api.alpaca.markets`
- `AlpacaStreamEnabled` = false (parse `ALPACA_STREAM_ENABLED` as true only for `1|true|TRUE`)

- [ ] **Step 2: Implement config fields + Load()**

- [ ] **Step 3: Write broker httptest tests**

```go
func TestSubmitOrderMarket(t *testing.T) {
    // ServeJSON POST /v2/orders → {"id":"oid","client_order_id":"42","symbol":"AAPL","side":"buy","qty":"1","filled_qty":"0","filled_avg_price":"0","status":"accepted"}
    // Client{BaseURL: srv.URL, Key, Secret, HTTP: srv.Client()}
    // SubmitOrder buy AAPL qty 1 client_order_id "42"
    // Assert request headers APCA-API-KEY-ID / SECRET and JSON body type=market time_in_force=day
}
func TestGetAccountAndPositions(t *testing.T) { /* GET /v2/account and /v2/positions */ }
func TestCachedClientTTL(t *testing.T) {
    // Counting handler: two GetAccount within TTL → 1 upstream call; after TTL → 2
}
```

- [ ] **Step 4: Implement `alpaca.go` + `cache.go`**

Map Alpaca side `buy`/`sell` to lowercase. Parse qty/price from JSON numbers-or-strings (Alpaca often returns strings).

`NewAlpaca(cfg)` returns error if key/secret empty: `fmt.Errorf("alpaca credentials required")`.

- [ ] **Step 5: Run tests**

Run: `cd services/api && go test ./internal/config/ ./internal/broker/ -count=1`  
Expected: PASS

- [ ] **Step 6: Commit (optional)**

```bash
git commit -m "feat: add Alpaca Paper broker client and config"
```

---

### Task 3: Order mirror model + `bypass_risk` + proposal `submitted`

**Files:**
- Modify: `services/api/internal/models/order.go`
- Modify: `services/api/internal/models/proposal.go` (comment statuses)
- Modify: `services/api/internal/workflow/steps.go`
- Modify: `services/api/internal/strategy/validate.go`
- Modify: `services/api/internal/strategy/validate_test.go`
- Modify: `apps/web/src/lib/types.ts` — `ExecutionMode` union includes `bypass_risk`
- Modify: `apps/web/src/app/(shell)/settings/page.tsx` — `<option value="bypass_risk">`

**Interfaces:**
- Produces: `workflow.ExecutionModeBypassRisk = "bypass_risk"`
- Produces: `workflow.ProposalSubmitted = "submitted"`
- Order mirror fields:

```go
BrokerOrderID string `gorm:"size:64;index" json:"broker_order_id"`
ClientOrderID string `gorm:"size:64;index" json:"client_order_id"`
ProposalID    *uint  `json:"proposal_id"`
```

Status may be `submitted|filled|rejected|canceled` (not only `filled`).

- [ ] **Step 1: Failing validate test**

```go
func TestValidateBypassRiskOK(t *testing.T) {
    s := validStrategy()
    s.ExecutionMode = ExecutionModeBypassRisk // "bypass_risk"
    if err := ValidateStrategyFields(s); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Implement constants + validation + model fields + AutoMigrate already covers Order via existing db.AutoMigrate**

- [ ] **Step 3: Settings UI option**

```tsx
<option value="bypass_risk">bypass_risk (skip Go risk)</option>
```

- [ ] **Step 4: Tests**

Run: `cd services/api && go test ./internal/strategy/ ./internal/models/ -count=1`  
Run: `cd apps/web && npm test -- --run src/app/(shell)/settings/page.test.tsx` (or full suite)  
Expected: PASS

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "feat: add bypass_risk mode and broker order mirror fields"
```

---

### Task 4: Workflow runner submits to Alpaca (replace ApplyFill)

**Files:**
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/workflow/runner_test.go`
- Modify: `services/api/internal/workflow/steps.go` (if helpers needed)
- Wire: `Runner.Broker broker.Client` (required in production; tests use fake)

**Interfaces:**
- Consumes: `broker.Client`
- Produces: on allowed proposal → `SubmitOrder` then mirror `models.Order` + proposal `submitted`; then poll `GetOrder` until terminal (filled/canceled/rejected) or timeout (e.g. 15s, 250ms sleep) → update proposal `filled|rejected` and order mirror fill fields
- Risk marks: prefer broker position `CurrentPrice`, else data-step close (existing marks map) for gate notional only — **not** used as fill price
- When `executionMode == ExecutionModeBypassRisk`: skip `Risk.Evaluate`; submit all non-empty proposals
- When broker `SubmitOrder` errors: set proposal `rejected`, store reason in `BreachReasonsJSON` as `["broker: ..."]`, continue other proposals (partial success)
- Remove production calls to `r.Ledger.ApplyFill` in this path; keep `UpsertNAV` using broker equity or marks after sync

- [ ] **Step 1: Add fake broker in tests**

```go
type fakeBroker struct {
    submit func(broker.OrderRequest) (broker.Order, error)
    get    map[string]broker.Order
    acct   broker.Account
    pos    []broker.Position
}
// implement Client
```

- [ ] **Step 2: Failing tests**

1. `TestRunEODBypassRiskSubmitsWithoutRisk` — Risk engine that would always breach; mode bypass; expect SubmitOrder called and proposal filled from broker get status `filled`.
2. `TestRunEODAutoRejectStillRejects` — breach + auto_reject → no SubmitOrder.
3. `TestRunEODRequireApprovalDoesNotSubmit` — breach + require_approval → approval row, no submit.
4. `TestRunEODPassSubmitsToBroker` — pass risk → submit + filled.

- [ ] **Step 3: Implement submit/sync helpers in runner**

```go
func (r *Runner) submitAndSync(ctx context.Context, accountID uint, run *models.WorkflowRun, p *models.TradeProposal) error {
    clientOrderID := fmt.Sprintf("prop-%d", p.ID)
    bo, err := r.Broker.SubmitOrder(ctx, broker.OrderRequest{
        Symbol: p.Symbol, Side: p.Side, Qty: p.Qty,
        ClientOrderID: clientOrderID, Type: "market", TimeInForce: "day",
    })
    // persist mirror status submitted; update proposal submitted
    // poll GetOrder until terminal / timeout
}
```

For risk `portfolioState` after submit: reload from `Broker.ListPositions` + `GetAccount` when Broker set; else fall back to DB for unit tests without broker account sync — prefer broker when non-nil.

- [ ] **Step 4: Run tests**

Run: `cd services/api && go test ./internal/workflow/ -count=1`  
Expected: PASS

- [ ] **Step 5: Commit (optional)**

```bash
git commit -m "feat: route workflow fills through Alpaca Paper broker"
```

---

### Task 5: Approvals approve → Alpaca submit

**Files:**
- Modify: `services/api/internal/approvals/service.go`
- Modify: `services/api/internal/approvals/service_test.go`
- Modify: `services/api/cmd/api/main.go` / router wiring — inject broker

**Interfaces:**
- On `approved`: call same submit/sync as runner (extract shared helper in `workflow` or `brokerexec` package to avoid duplication — prefer `workflow.SubmitProposal(ctx, db, broker, ...)` used by both)
- Do **not** call `ledger.ApplyFillTx`
- Reject path unchanged
- After decide, `UpsertNAV` may use broker equity

- [ ] **Step 1: Failing test** — approve pending approval with fake broker → proposal filled, mirror order has `BrokerOrderID`, ledger cash unchanged if previously asserted

- [ ] **Step 2: Implement**

- [ ] **Step 3: Run** `go test ./internal/approvals/ ./internal/workflow/ -count=1`

- [ ] **Step 4: Commit (optional)** — `feat: submit Alpaca orders on approval`

---

### Task 6: Overview / Portfolio / Orders from Alpaca

**Files:**
- Modify: `services/api/internal/httpserver/handlers_overview.go`
- Modify: `services/api/internal/httpserver/handlers_portfolio.go`
- Create: `services/api/internal/httpserver/handlers_orders.go`
- Modify: `services/api/internal/httpserver/router.go` — `GET /api/v1/orders`
- Modify: `services/api/internal/httpserver/api_smoke_test.go` (broker fake)
- Modify: `services/api/internal/httpserver/router.go` API struct — `Broker broker.Client`

**Interfaces:**
- Overview: `cash`/`equity`/`nav` from `Broker.GetAccount` (+ position summary from `ListPositions`); keep `pending_approvals_count`, `latest_run`, `nav_series` from DB
- Portfolio: cash + positions from broker; map `AvgCost`/`CurrentPrice`/`UnrealizedPL`; `stop_loss`/`take_profit` remain from local `models.Position` if symbol matches (optional left join) — if none, null
- Orders: `{ orders: [ { broker_order_id, client_order_id, symbol, side, qty, filled_qty, filled_avg_price, status, proposal_id? } ] }` merging Alpaca list with local mirror by `client_order_id` / `broker_order_id`
- If broker nil or credentials missing: `503` `{"error":"alpaca not configured"}`

- [ ] **Step 1: Failing handler tests with fake broker returning known cash/positions**

- [ ] **Step 2: Implement handlers**

- [ ] **Step 3: Run** `go test ./internal/httpserver/ -count=1`

- [ ] **Step 4: Commit (optional)** — `feat: serve overview/portfolio/orders from Alpaca`

---

### Task 7: Frontend tiered polling

**Files:**
- Create: `apps/web/src/lib/useTieredPolling.ts`
- Create: `apps/web/src/lib/useTieredPolling.test.ts`
- Modify: `apps/web/src/app/(shell)/page.tsx`
- Modify: `apps/web/src/app/(shell)/portfolio/page.tsx`
- Optional: create `apps/web/src/app/(shell)/orders/page.tsx` if no orders page exists — minimal table calling `GET /api/v1/orders` with 5–10s poll when any status is open; else skip page and poll orders only from overview later — **prefer add thin Orders page** under shell nav if nav has a slot; otherwise poll on Portfolio only

**Interfaces:**

```ts
export type Tier = "account" | "orders";

export function pollIntervalMs(
  tier: Tier,
  phase: UsMarketSnapshot["phase"],
): number | null {
  // account: open → 20_000; else → 180_000
  // orders: always 8_000 while enabled by caller; null to stop
}

export function useTieredPolling(
  enabled: boolean,
  tier: Tier,
  tick: () => void | Promise<void>,
): void;
// uses getUsMarketSnapshot, document.visibilityState, setInterval; clears on unmount
```

- [ ] **Step 1: Vitest for `pollIntervalMs`**

```ts
expect(pollIntervalMs("account", "open")).toBe(20_000);
expect(pollIntervalMs("account", "weekend")).toBe(180_000);
expect(pollIntervalMs("orders", "open")).toBe(8_000);
```

- [ ] **Step 2: Implement hook** — on `document.hidden`, do not fire ticks (still keep interval or pause — prefer pause: clear interval while hidden, restart on visible)

- [ ] **Step 3: Wire Overview + Portfolio** to call existing loaders via `useTieredPolling(true, "account", load)`

- [ ] **Step 4: Run** `cd apps/web && npm test -- --run`

- [ ] **Step 5: Commit (optional)** — `feat: tiered polling for overview and portfolio`

---

### Task 8: Phase 1 docs

**Files:**
- Modify: `docs/eod-workflow-flowchart.md` — Alpaca submit; `bypass_risk`; Paper authority
- Modify: `README.md`, `deploy/README.md` — env table, authority notes, remove “alpaca stub”
- Modify: `docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md` — short note at ledger section: cash/positions/orders authority superseded by `2026-07-28-alpaca-paper-authority-design.md`

- [ ] **Step 1: Update docs to match shipped Phase 1 behavior**

- [ ] **Step 2: Commit (optional)** — `docs: Alpaca Paper authority phase 1`

---

## Phase 2 — Streaming (after Phase 1)

### Task 9: Stream hub (Go)

**Files:**
- Create: `services/api/internal/stream/hub.go`
- Create: `services/api/internal/stream/hub_test.go`
- Config already has `AlpacaStreamEnabled`

**Interfaces:**
- `Hub` maintains optional upstream connections; `Subscribe(ch chan []byte) (unsubscribe func)`
- Methods: `PublishQuote(symbol string, payload []byte)`, throttle ≥1s/symbol
- When disabled or no creds: `Hub.Enabled() bool` false

- [ ] Tests with fake pump injecting messages → subscriber receives throttled events
- [ ] Implement + `go test ./internal/stream/ -count=1`

### Task 10: SSE endpoints

**Files:**
- Create: `services/api/internal/httpserver/handlers_stream.go`
- Modify: `router.go` — `GET /api/v1/stream/market`, `GET /api/v1/stream/account` (JWT)
- Wire hub start in `main.go` when `AlpacaStreamEnabled`

**Behavior:**
- `Content-Type: text/event-stream`
- If `!Enabled`: `503 {"error":"streaming disabled"}`
- Heartbeat comment every 15s

### Task 11: Frontend SSE client

**Files:**
- Create: `apps/web/src/lib/useMarketStream.ts`
- Modify: Overview/Portfolio to merge quote prices when events arrive; keep REST polling as reconciliation
- Docs: set `ALPACA_STREAM_ENABLED=true` in env.example comments

- [ ] Vitest: reconnect backoff helper pure function
- [ ] Manual smoke note in deploy README

---

## Spec coverage checklist

| Spec item | Task |
|-----------|------|
| Alpaca daily bars | 1 |
| Go broker + config | 2 |
| Paper authority / no ApplyFill path | 4, 5 |
| Overview/Portfolio from Alpaca | 6 |
| Orders API | 6 |
| `bypass_risk` | 3, 4 |
| Tiered polling | 7 |
| Docs | 8 |
| SSE proxy | 9–11 |
| Secrets server-side | 2, 10 (no keys in web) |
| TTL cache | 2 (`cache.go`), 6 |
| `free` fallback | 1 (provider still selectable) |

---

## Self-review notes

- No TBD placeholders in task bodies.
- Broker interface names are consistent across Tasks 2–6 (`SubmitOrder`, `GetOrder`, `GetAccount`, `ListPositions`).
- Phase 2 explicitly gated on Phase 1 completion.
- Commit steps optional per repo convention.
