# Agent Runtime Tool-Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the five single-shot agent services with one `agent-runtime` (Analyst + Portfolio LangGraph tool-loops), persist visible tool traces, and keep Go risk + Alpaca Paper as execution authority.

**Architecture:** Go injects Alpaca account snapshot (cash/equity/positions/open_orders) + `risk_context`, then calls `POST {AGENT_RUNTIME_URL}/v1/run` twice (`agent=analyst` then `agent=portfolio`). Runtime returns `{result,trace}`; Go persists the full envelope and feeds only `result` forward. Runs UI renders a tool timeline from `trace.rounds`.

**Tech Stack:** Python 3.12, FastAPI, LangGraph, httpx, jsonschema, pytest; Go 1.22+ (existing Gin/Gorm/broker); Next.js Runs detail; Finnhub + Tavily (web search).

**Spec:** `docs/superpowers/specs/2026-07-28-agent-runtime-tool-loop-design.md`

## Global Constraints

- Agents never call Alpaca Trading; only Go places orders
- Topology: Analyst + Portfolio only; Python Risk off main path
- One `agent-runtime` process; two graphs
- `WEB_SEARCH_ENABLED` default **`true`**
- News/web-search tool failures degrade (`ok:false`); do not fail the step alone
- Invalid final schema / crash / max rounds without valid final → step failed → run `failed` → no Alpaca submit
- `MAX_TOOL_ROUNDS_ANALYST` default `8`; `MAX_TOOL_ROUNDS_PORTFOLIO` default `3`
- Bars lookback default `20`; Finnhub news top `3` per symbol
- CI must pass with `LLM_MODE=mock` and without Finnhub/Tavily keys
- Do not commit unless the user explicitly asks (Commit steps are optional gates)
- Run relevant pytest / `go test` / `npm test` before claiming a task done

---

## File map

| File | Responsibility |
|------|----------------|
| `packages/contracts/agent_run_request.schema.json` | Add `agent`, `risk_context`, extend `account_snapshot` with `open_orders` / `equity` |
| `packages/contracts/analyst_result.schema.json` | New analyst result schema |
| `packages/contracts/agent_run_response.schema.json` | Envelope `{result, trace}` |
| `packages/contracts/fixtures/analyst_result.valid.json` | Mock fixture |
| `packages/contracts/fixtures/agent_run_response.analyst.valid.json` | Envelope fixture |
| `services/agents/common/stock_agents_common/marketdata/*` | Lookback multi-day bars API |
| `services/agents/common/stock_agents_common/tools/*.py` | Tool implementations |
| `services/agents/common/stock_agents_common/trace.py` | Trace builders |
| `services/agents/common/stock_agents_common/llm_tools.py` | Chat completions with tool_calls |
| `services/agents/runtime/` | FastAPI app, graphs, Dockerfile |
| `services/api/internal/config/config.go` | `AGENT_RUNTIME_URL` |
| `services/api/internal/agentsclient/client.go` | Single RuntimeURL |
| `services/api/internal/workflow/steps.go` | `StepAnalyst`, chain analyst→portfolio |
| `services/api/internal/workflow/runner.go` | Envelope unwrap, Alpaca snapshot, marks without data step |
| `services/api/internal/approvals/service.go` | `loadMarks` without data step |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | Tool timeline UI |
| `deploy/docker-compose.yml`, `deploy/.env` / `env.example`, docs | Wire runtime; retire five agents |

---

### Task 1: Contracts — request/response + analyst_result

**Files:**
- Modify: `packages/contracts/agent_run_request.schema.json`
- Create: `packages/contracts/analyst_result.schema.json`
- Create: `packages/contracts/agent_run_response.schema.json`
- Create: `packages/contracts/fixtures/analyst_result.valid.json`
- Create: `packages/contracts/fixtures/agent_run_request.valid.json` (update existing)
- Create: `packages/contracts/fixtures/agent_run_response.analyst.valid.json`
- Modify: `packages/contracts/scripts/validate_fixtures.py` if it auto-discovers fixtures

**Interfaces:**
- Produces: schemas used by runtime validation and Go docs
- Consumes: none

- [ ] **Step 1: Extend `agent_run_request.schema.json`**

Add to `required`: keep existing; add `"agent"`.

```json
"agent": { "type": "string", "enum": ["analyst", "portfolio"] },
"account_snapshot": {
  "type": "object",
  "required": ["cash", "currency", "positions"],
  "properties": {
    "cash": { "type": "number" },
    "equity": { "type": "number" },
    "currency": { "type": "string", "const": "USD" },
    "positions": { "...existing..." },
    "open_orders": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "symbol", "side", "qty", "status"],
        "properties": {
          "id": { "type": "string" },
          "symbol": { "type": "string" },
          "side": { "type": "string" },
          "qty": { "type": "number" },
          "status": { "type": "string" },
          "client_order_id": { "type": "string" }
        },
        "additionalProperties": false
      }
    }
  }
},
"risk_context": {
  "type": "object",
  "properties": {
    "execution_mode": { "type": "string" },
    "rules": {
      "type": "object",
      "properties": {
        "max_order_notional": { "type": "number" },
        "max_single_name_weight": { "type": "number" },
        "min_cash_ratio": { "type": "number" }
      },
      "additionalProperties": true
    }
  },
  "additionalProperties": true
},
"limits": {
  "type": "object",
  "properties": {
    "max_tool_rounds": { "type": "integer", "minimum": 1 },
    "timeout_sec": { "type": "integer", "minimum": 1 }
  },
  "additionalProperties": false
}
```

Keep `additionalProperties: false` on the root; include new properties explicitly.

- [ ] **Step 2: Add `analyst_result.schema.json`**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "analyst_result.schema.json",
  "type": "object",
  "required": ["items"],
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["symbol", "bias", "confidence", "thesis", "side", "urgency", "rationale"],
        "properties": {
          "symbol": { "type": "string" },
          "bias": { "type": "string", "enum": ["bull", "bear", "neutral"] },
          "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
          "thesis": { "type": "string" },
          "side": { "type": "string", "enum": ["buy", "sell", "hold"] },
          "urgency": { "type": "string", "enum": ["low", "normal", "high"] },
          "rationale": { "type": "string" },
          "evidence": { "type": "array", "items": { "type": "string" } }
        },
        "additionalProperties": false
      }
    },
    "warnings": { "type": "array", "items": { "type": "string" } }
  },
  "additionalProperties": false
}
```

- [ ] **Step 3: Add `agent_run_response.schema.json`**

Envelope: required `result` (object), `trace` with `agent`, `rounds` (array), `stop_reason` enum `final|max_rounds|timeout|error`. Allow flexible round objects (`additionalProperties: true` on round items) so LangGraph evolution does not break contract validation of the envelope shell. Validate `result` separately against `analyst_result` / `portfolio_result` by agent.

- [ ] **Step 4: Fixtures + validate**

```bash
cd packages/contracts
python scripts/validate_fixtures.py
```

Expected: PASS for all fixtures including new ones.

- [ ] **Step 5: Commit (optional)** `feat(contracts): analyst result and tool-loop run envelope`

---

### Task 2: Multi-day bars on marketdata providers

**Files:**
- Modify: `services/agents/common/stock_agents_common/marketdata/base.py`
- Modify: `services/agents/common/stock_agents_common/marketdata/alpaca.py`
- Modify: `services/agents/common/stock_agents_common/marketdata/free.py`
- Modify: `services/agents/common/tests/test_marketdata_alpaca.py`
- Modify: `services/agents/common/tests/test_marketdata_free.py`

**Interfaces:**
- Produces: `get_daily_bars(symbols, trade_date, *, lookback_days: int = 1) -> list[dict]`  
  Each bar still has `symbol,trade_date,open,high,low,close,volume`. Multiple dates per symbol when lookback > 1.
- Consumes: existing Alpaca/Yahoo HTTP

- [ ] **Step 1: Failing tests for lookback=3 returning multiple dates**

```python
def test_alpaca_lookback_returns_multiple_bars(monkeypatch):
    # mock Alpaca bars array length 3 for AAPL; assert 3 mapped bars with distinct trade_date
    ...
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd services/agents/common && python -m pytest tests/test_marketdata_alpaca.py -v
```

- [ ] **Step 3: Implement lookback**

Alpaca: `start = trade_date - (lookback_days + weekend buffer) days`, `end = trade_date + 1 day`, take last `lookback_days` bars per symbol.

Free/Yahoo: request enough history; slice last N sessions.

Keep `lookback_days=1` behavior identical to today (single bar on `trade_date` when present).

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Commit (optional)** `feat(marketdata): multi-day daily bars lookback`

---

### Task 3: Tools + trace helpers (no graph yet)

**Files:**
- Create: `services/agents/common/stock_agents_common/trace.py`
- Create: `services/agents/common/stock_agents_common/tools/__init__.py`
- Create: `services/agents/common/stock_agents_common/tools/bars.py`
- Create: `services/agents/common/stock_agents_common/tools/news.py`
- Create: `services/agents/common/stock_agents_common/tools/web_search.py`
- Create: `services/agents/common/stock_agents_common/tools/account.py`
- Create: `services/agents/common/stock_agents_common/tools/sizing.py`
- Create: `services/agents/common/tests/test_tools_news.py`
- Create: `services/agents/common/tests/test_tools_web_search.py`
- Create: `services/agents/common/tests/test_tools_sizing.py`

**Interfaces:**
- Each tool: `(ctx: RunContext, **args) -> dict` where failure returns `{"ok": False, "error": "..."}` and success `{"ok": True, "data": ...}`
- `RunContext` holds `req` dict (snapshot, risk_context, trade_date, watchlist) + optional http clients
- `get_news(symbol, from_date=None, to_date=None)` → Finnhub `/api/v1/company-news`
- `web_search(query, limit=5)` → Tavily when `WEB_SEARCH_ENABLED` not false; if no key → `{ok:false, error:"missing_web_search_api_key"}`
- `size_proposals` — move deterministic logic from `services/agents/portfolio/app/main.py` `size_proposals` (adapt to take analyst items or intents)

- [ ] **Step 1: Write failing tool tests** (httpx MockTransport for Finnhub/Tavily; sizing unit tests without LLM)

- [ ] **Step 2: Run — expect FAIL**

```bash
cd services/agents/common && python -m pytest tests/test_tools_*.py -v
```

- [ ] **Step 3: Implement tools + `trace.new_trace(agent)` / `append_round(...)`**

Env:
- `FINNHUB_API_KEY`
- `WEB_SEARCH_ENABLED` default true (`""` or unset ⇒ true; only `false`/`0`/`no` disables)
- `WEB_SEARCH_PROVIDER` default `tavily`
- `WEB_SEARCH_API_KEY`

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit (optional)** `feat(agents): readonly tools and trace helpers`

---

### Task 4: LLM tool-calling client + mock mode

**Files:**
- Create: `services/agents/common/stock_agents_common/llm_tools.py`
- Create: `services/agents/common/tests/test_llm_tools_mock.py`
- Modify: `services/agents/common/stock_agents_common/llm.py` only if shared helpers help (prefer not to break existing agents until removed)

**Interfaces:**
- `ToolLLMClient.complete_tools(system, messages, tools_openai_schema) -> {content, tool_calls, usage, latency_ms}`
- Mock mode (`LLM_MODE=mock`): scripted sequence from env `MOCK_TOOL_SCRIPT` JSON **or** fixture file under `packages/contracts/fixtures/mock_tool_scripts/analyst.json` that lists rounds: either tool_calls or final JSON content

Recommended mock script shape:

```json
{
  "rounds": [
    { "tool_calls": [{ "id": "1", "name": "get_account_view", "args": {} }] },
    { "tool_calls": [{ "id": "2", "name": "get_daily_bars", "args": { "lookback_days": 20 } }] },
    { "content_json": { "items": [ /* valid analyst_result items for watchlist */ ] } }
  ]
}
```

- [ ] **Step 1: Failing test** — mock client returns tool_calls then final JSON

- [ ] **Step 2: Implement mock + real OpenAI-compatible `tools` / `tool_calls` path**

Real path: `chat/completions` with `tools`, parse `message.tool_calls`; final round may use `response_format: json_object` when no tools requested (finalize).

- [ ] **Step 3: pytest PASS**

- [ ] **Step 4: Commit (optional)** `feat(agents): tool-calling LLM client with mock scripts`

---

### Task 5: AnalystGraph + PortfolioGraph in `agent-runtime`

**Files:**
- Create: `services/agents/runtime/pyproject.toml` or `requirements.txt`
- Create: `services/agents/runtime/app/main.py`
- Create: `services/agents/runtime/app/graphs/analyst.py`
- Create: `services/agents/runtime/app/graphs/portfolio.py`
- Create: `services/agents/runtime/app/graphs/loop.py` (shared LangGraph wiring)
- Create: `services/agents/runtime/Dockerfile`
- Create: `services/agents/runtime/tests/test_analyst_graph.py`
- Create: `services/agents/runtime/tests/test_portfolio_graph.py`
- Create: `services/agents/runtime/tests/test_http.py`

**Interfaces:**
- `POST /v1/run` body = `agent_run_request` → response = `{result, trace}` validated
- Analyst tools: `get_daily_bars`, `get_news`, `web_search`, `get_account_view`, `get_risk_context`
- Portfolio tools: `get_account_view`, `get_risk_context`, `get_last_closes`, `size_proposals`
- LangGraph: `call_model` ↔ `tools` until no tool_calls or `i >= max_rounds`; then validate result schema

Dependencies (runtime requirements):

```
fastapi>=0.115
uvicorn[standard]>=0.30
langgraph>=0.2
langchain-core>=0.3
httpx>=0.27
jsonschema>=4.0
-e ../common
```

- [ ] **Step 1: Failing HTTP/graph tests under `LLM_MODE=mock`**

```python
def test_analyst_run_returns_result_and_trace(client, monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")
    # point mock script at fixture covering watchlist AAPL
    resp = client.post("/v1/run", json={...agent: analyst...})
    assert resp.status_code == 200
    body = resp.json()
    assert "trace" in body and body["trace"]["rounds"]
    assert body["trace"]["stop_reason"] in {"final", "max_rounds"}
    validate(body["result"], "analyst_result")
```

- [ ] **Step 2: Implement shared loop + both graphs + FastAPI router on `agent` field**

Align analyst items to full watchlist (default hold/neutral) like current research/decision helpers.

Portfolio: always call `size_proposals` at least once (deterministic baseline); mock may return baseline without refine.

- [ ] **Step 3: Dockerfile EXPOSE 8001** (single port for runtime)

```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY packages/contracts /app/packages/contracts
COPY services/agents/common /app/services/agents/common
COPY services/agents/runtime /app/services/agents/runtime
RUN pip install --no-cache-dir -e /app/services/agents/common \
 && pip install --no-cache-dir -r /app/services/agents/runtime/requirements.txt
WORKDIR /app/services/agents/runtime
EXPOSE 8001
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]
```

- [ ] **Step 4: pytest PASS**

```bash
cd services/agents/runtime && python -m pytest -v
```

- [ ] **Step 5: Commit (optional)** `feat(runtime): LangGraph analyst and portfolio tool loops`

---

### Task 6: Go — config, agents client, snapshot builder

**Files:**
- Modify: `services/api/internal/config/config.go`
- Modify: `services/api/internal/agentsclient/client.go`
- Modify: wherever Client is constructed (main/httpserver wiring)
- Create: `services/api/internal/workflow/agent_snapshot.go` (or methods on Runner)
- Create: `services/api/internal/workflow/agent_snapshot_test.go`

**Interfaces:**
- Config: `AgentRuntimeURL string` from `AGENT_RUNTIME_URL`; keep old URL fields temporarily unused or remove if all call sites updated in Task 7
- `buildAgentSnapshot(ctx) (AccountSnapshot, error)` using `Broker.GetAccount`, `ListPositions`, `ListOrders(ctx, "open")`
- `buildRiskContext(executionMode string) map/struct` from config risk thresholds

Snapshot JSON tags must match schema (`open_orders`, `equity`, etc.). Extend `ledger.AccountSnapshot` **or** define `workflow.AgentAccountSnapshot` used only for agent requests (prefer dedicated type so ledger DB snapshot stays unchanged).

- [ ] **Step 1: Failing test** with fakeBroker returning account/positions/orders → snapshot shape

- [ ] **Step 2: Implement builder + config**

- [ ] **Step 3: `go test ./internal/config ./internal/agentsclient ./internal/workflow -count=1` PASS for new tests**

- [ ] **Step 4: Commit (optional)** `feat(api): agent runtime URL and Alpaca agent snapshot`

---

### Task 7: Go — runner chain + marks without data step + approvals

**Files:**
- Modify: `services/api/internal/workflow/steps.go`
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/workflow/runner_test.go`
- Modify: `services/api/internal/approvals/service.go`
- Modify: `services/api/internal/approvals/service_test.go`
- Modify: any smoke tests referencing five steps

**Critical marks migration (spec gap fill):**

After removing `data` step, marks must not come from `prior[data].bars`.

Use this order:

1. `marks := map[string]float64{}`
2. `marks = r.mergeBrokerMarks(ctx, marks)` (position `CurrentPrice`)
3. For each portfolio proposal, if mark missing and `qty > 0` and `estimated_notional > 0`, set `marks[symbol] = estimated_notional / qty`
4. If still missing for a proposal symbol → error as today

Approvals `loadMarksTx`: **stop reading StepData**. Prefer:

1. Broker marks via service broker if available, else
2. Derive from run’s proposals (`estimated_notional/qty`), else
3. Return `ErrMissingFillPrice`

**Envelope handling in runner loop:**

```go
type agentEnvelope struct {
    Result json.RawMessage `json:"result"`
    Trace  json.RawMessage `json:"trace"`
}

// persist full raw body
// unmarshal envelope; prior[step.Name] = decoded Result object (map)
// for portfolio validate: validatePortfolioResult(envelope.Result)
```

**AgentChain:**

```go
const (
  StepAnalyst   = "analyst"
  StepPortfolio = "portfolio"
  // keep old constants for reading historical rows in UI if needed
)

func AgentChain() []AgentStep {
  return []AgentStep{
    {Name: StepAnalyst, Timeout: LLMAgentTimeout}, // consider 180s for tool loops
    {Name: StepPortfolio, Timeout: LLMAgentTimeout},
  }
}
```

`agentURL`: always `r.Agents.RuntimeURL` (ignore step-specific URLs).

Request body: set `Agent: step.Name`, `AccountSnapshot: agentSnap`, `RiskContext: ...`.

When calling portfolio, `PriorStepOutputs` should contain `"analyst": <analyst result object>` (not the envelope).

- [ ] **Step 1: Update runner_test stubs** to two httptest servers returning envelopes; assert chain length 2; assert marks work without bars

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd services/api && go test ./internal/workflow/ ./internal/approvals/ -count=1
```

- [ ] **Step 3: Implement runner + approvals changes**

- [ ] **Step 4: Run tests — expect PASS**; fix api_smoke_test step names

- [ ] **Step 5: Commit (optional)** `feat(api): analyst/portfolio chain with tool-loop envelopes`

---

### Task 8: Web — Runs tool timeline

**Files:**
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.test.tsx`
- Modify: `apps/web/src/lib/types.ts` if needed

**UI behavior (spec §6):**

1. Parse `payload_json`; if `result`+`trace`, show result summary + **Show tool trace** + **Show raw payload**
2. Tool trace: for each `trace.rounds[i]`, render round index, tool names, `ok`, `latency_ms`, truncated `result_preview` / `error`
3. Show `stop_reason` near step header when present
4. Legacy payloads (old five-step JSON without envelope) still pretty-print via raw

- [ ] **Step 1: Vitest** with fixture envelope payload asserting trace section toggles

- [ ] **Step 2: Implement components** (`StepResultSummary`, `StepToolTrace`)

- [ ] **Step 3: `cd apps/web && npm test` PASS**

- [ ] **Step 4: Commit (optional)** `feat(web): render agent tool traces on run detail`

---

### Task 9: Deploy, env, docs, retire five agents

**Files:**
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.override.yml`
- Modify: `deploy/env.example` (or `deploy/.env` sample — do not commit secrets)
- Modify: `deploy/e2e_api.ps1` / `deploy/e2e_api.sh` if they assume five agents
- Modify: `docs/product-overview.md`
- Modify: `docs/eod-workflow-flowchart.md`
- Modify: `deploy/README.md`
- Modify: `README.md` agent list
- Optional: leave old agent Dockerfiles in tree but remove from compose (delete services in a follow-up if desired)

**Compose api env:**

```yaml
agent-runtime:
  build:
    context: ..
    dockerfile: services/agents/runtime/Dockerfile
  env_file: [./.env]
  environment:
    LLM_MODE: ${LLM_MODE:-mock}
    MARKET_DATA_PROVIDER: ${MARKET_DATA_PROVIDER:-alpaca}
    WEB_SEARCH_ENABLED: ${WEB_SEARCH_ENABLED:-true}
    MAX_TOOL_ROUNDS_ANALYST: ${MAX_TOOL_ROUNDS_ANALYST:-8}
    MAX_TOOL_ROUNDS_PORTFOLIO: ${MAX_TOOL_ROUNDS_PORTFOLIO:-3}

api:
  environment:
    AGENT_RUNTIME_URL: http://agent-runtime:8001
  depends_on:
    agent-runtime:
      condition: service_started
```

Remove `agent-data|research|decision|portfolio|risk` services and old `AGENT_*_URL`s from api.

- [ ] **Step 1: Update compose + env.example keys** (`FINNHUB_API_KEY`, `WEB_SEARCH_API_KEY`, `WEB_SEARCH_PROVIDER=tavily`, `WEB_SEARCH_ENABLED=true`, `AGENT_RUNTIME_URL`)

- [ ] **Step 2: Update product docs / flowchart** to analyst→portfolio + tool loop + Go risk

- [ ] **Step 3: Smoke**

```bash
# from repo practices in deploy/README
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.override.yml up --build -d
# hit healthz agent-runtime + api; optional e2e script with LLM_MODE=mock
```

- [ ] **Step 4: Commit (optional)** `chore(deploy): switch stack to agent-runtime tool-loop`

---

## Self-review checklist (plan author)

| Spec requirement | Task |
|------------------|------|
| Single runtime, two graphs | 5, 9 |
| Tool traces in payload + UI | 5, 7, 8 |
| Finnhub news + degrade | 3, 5 |
| Web search default on | 3, 9 |
| Alpaca snapshot + open_orders | 6, 7 |
| risk_context injected | 6, 7 |
| Go risk still final; no Python risk step | 7 |
| Marks without data bars | 7 (explicit) |
| Contracts analyst_result + envelope | 1 |
| Multi-day bars | 2 |
| Retire five agents from compose | 9 |

**Type consistency:** step name `analyst` / `portfolio`; response always `{result,trace}`; prior map stores **result only**.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-28-agent-runtime-tool-loop.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks  
2. **Inline Execution** — execute tasks in this session with executing-plans checkpoints  

Which approach?
