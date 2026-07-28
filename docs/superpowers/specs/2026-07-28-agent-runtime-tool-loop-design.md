# Agent Runtime Tool-Loop — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation planning  
**Related:** `2026-07-28-alpaca-paper-authority-design.md`, `2026-07-28-strategy-scheduler-runs-observability-design.md`, `docs/product-overview.md`, `docs/eod-workflow-flowchart.md`

## 1. Goal

Replace the fixed five-step “pseudo-agent” pipeline (single-shot JSON LLM + prefetched one-day OHLCV) with a **single Python `agent-runtime`** that runs **two thin LangGraph tool-loops** (`analyst`, `portfolio`). Agents may fetch market data, news, and web search via tools until satisfied (or limits hit). **Tool traces must be visible in the Runs UI.** Go remains the orchestrator, Alpaca Paper authority, and deterministic risk gate.

### 1.1 Success criteria

- One Compose service `agent-runtime` exposes `POST /v1/run` routed by `agent: analyst | portfolio`.
- Analyst can call tools in a loop (`get_daily_bars`, `get_news`, `web_search`, account/risk views) and emit per-symbol thesis + intent.
- Portfolio sizes executable proposals (deterministic `size_proposals` primary; short optional LLM refine).
- Each step response is `{ result, trace }`; Runs detail shows a **tool timeline** (rounds, tool name/args/ok/latency/preview), not only raw JSON.
- News / web-search tool failures **degrade** (trace `ok:false`); they do not fail the run by themselves.
- Invalid final schema / runtime crash / exhausted rounds without valid final → step failed → run `failed` → **no Alpaca submit**.
- Go risk rules + `execution_mode` remain the only hard gate; agents never call Alpaca Trading.

### 1.2 Out of scope

- CrewAI or heavy multi-agent chat frameworks
- Live (non-paper) trading
- Intraday minute bars / tick streams as primary input
- Letting LLM bypass Go risk or place orders
- Browser-direct LLM or tool credentials
- Replacing Go scheduler / approvals / Alpaca submit path

## 2. Decisions (locked)

| Topic | Choice |
|-------|--------|
| Topology | **Two roles:** Analyst + Portfolio; Python Risk advisory **removed from main path** |
| Deployment | **One** `agent-runtime` process (two graphs); not five containers |
| Loop runtime | Thin **LangGraph** tool-loop with hard `max_tool_rounds` |
| Observability | Structured `trace` in step `payload_json`; Runs UI dedicated tool timeline |
| Account context | Go injects **Alpaca-authoritative** snapshot: cash, equity, positions, **open_orders** |
| Risk context | Go injects read-only `risk_context` (execution_mode + rule thresholds) |
| News | **Finnhub** `GET /company-news` via `FINNHUB_API_KEY` |
| Web search | **Enabled by default** (`WEB_SEARCH_ENABLED=true`); provider via env (see §5) |
| News/search failure | Degrade; continue loop / hold when evidence weak |
| Bars lookback | Default **20** daily bars |
| News per symbol | Default top **3** (headline, summary, datetime, source, url) |
| Analyst max rounds | Default **8** |
| Portfolio max rounds | Default **3** |
| Old chain | `data → research → decision → portfolio → risk` **retired** from main path |

## 3. Architecture

```text
Web Runs UI ── tool timeline ──► step.payload_json { result, trace }
                                      ▲
Scheduler / Run now                   │ persist
  → Go Runner
       │ Inject account_snapshot (Alpaca) + watchlist + risk_context
       ├─ POST agent-runtime { agent: "analyst" }
       │     AnalystGraph: LLM ⇄ tools → { result, trace }
       ├─ POST agent-runtime { agent: "portfolio", prior.analyst }
       │     PortfolioGraph: size_proposals (+ short loop) → { result, trace }
       └─ Go risk_rule_configs + execution_mode → Alpaca Paper
```

### 3.1 Trust boundaries

| Actor | May | Must not |
|-------|-----|----------|
| Analyst / Portfolio | Read-only tools; propose structured JSON | Call Alpaca Trading; mutate cash/positions/orders; skip Go risk |
| Go API | Orchestrate, inject snapshots, persist steps, risk gate, submit/sync orders | Expose `ALPACA_*` / `FINNHUB_*` / `LLM_*` to browser |
| Finnhub / web search / market data | Serve read data to runtime tools | Receive trading credentials |

### 3.2 Component changes

| Component | Change |
|-----------|--------|
| `services/agents/runtime` (new) | LangGraph loops, tools, `/v1/run`, shared LLM client |
| Go `workflow.Runner` | Chain `analyst` → `portfolio`; build Alpaca snapshot + open_orders + risk_context |
| Go config | `AGENT_RUNTIME_URL` replaces per-agent URLs |
| Compose | Add `agent-runtime`; remove `agent-data/research/decision/portfolio/risk` from main path |
| Contracts | `analyst_result` (+ response envelope with `trace`); extend request schema; keep `portfolio_result` |
| Web Runs detail | Result summary + **Show tool trace** timeline + raw payload fallback |
| Docs | product-overview, flowchart, deploy README |

## 4. Contracts

### 4.1 Request (`agent_run_request` extended)

Required (existing + new):

- `run_id`, `trade_date`, `watchlist`, `account_snapshot`
- `agent`: `"analyst"` \| `"portfolio"`

`account_snapshot` (Alpaca-authoritative for agent use):

- `cash`, `equity` (optional but recommended), `currency: "USD"`
- `positions[]`: `symbol`, `qty`, `avg_cost`, optional stops
- `open_orders[]`: `id`, `symbol`, `side`, `qty`, `status`, optional `client_order_id`

Also:

- `risk_context`: `{ execution_mode, rules: { max_notional?, max_symbol_weight?, min_cash_pct? } }` (field names aligned with existing rule keys)
- `prior_step_outputs`: for portfolio, include analyst **`result`** only (not full prior traces in prompt)
- `limits` (optional): `max_tool_rounds`, `timeout_sec`

### 4.2 Response envelope (every `/v1/run`)

```json
{
  "result": {},
  "trace": {
    "agent": "analyst",
    "started_at": "ISO-8601",
    "ended_at": "ISO-8601",
    "rounds": [
      {
        "i": 1,
        "llm": { "model": "…", "latency_ms": 0 },
        "assistant": {
          "content": "…",
          "tool_calls": [{ "id": "c1", "name": "get_news", "args": { "symbol": "AAPL" } }]
        },
        "tools": [
          {
            "id": "c1",
            "name": "get_news",
            "ok": true,
            "latency_ms": 0,
            "result_preview": "…",
            "error": null
          }
        ]
      }
    ],
    "stop_reason": "final",
    "usage": { "prompt_tokens": 0, "completion_tokens": 0 }
  }
}
```

`stop_reason`: `final` \| `max_rounds` \| `timeout` \| `error`.

Go persists the full envelope in `workflow_step_results.payload_json`. First version stores traces in Postgres (watchlist size is small). Truncate `result_preview` per tool (e.g. 2KB) if needed.

### 4.3 Analyst `result`

Per watchlist symbol (required coverage; missing → default hold/neutral):

- `symbol`, `bias` (`bull`\|`bear`\|`neutral`), `confidence` (0–1)
- `thesis`, `side` (`buy`\|`sell`\|`hold`), `urgency` (`low`\|`normal`\|`high`), `rationale`
- optional `evidence[]`: short strings referencing tools used

Schema name: `analyst_result`.

### 4.4 Portfolio `result`

Reuse / lightly extend `portfolio_result`:

- `proposals[]`: `symbol`, `side` (`buy`\|`sell`), `qty`, `stop_loss`, `take_profit`, `estimated_notional`, `estimated_cash_impact`, optional `target_weight`
- optional `warnings[]`

Zero proposals is valid.

### 4.5 Failure semantics

| Case | Behavior |
|------|----------|
| Single tool failure (Finnhub, web search, bars miss) | `trace` entry `ok:false`; loop may continue; prefer hold if evidence weak |
| Valid `result` after degrade | Step OK |
| Schema invalid / crash / max rounds without valid final | Step failed → run `failed` → no broker submit |
| Go risk breach | Existing `execution_mode` behavior unchanged |

### 4.6 Retired from main path

`data_result`, `research_result`, `decision_result`, `risk_advisory_result` are not produced by the new chain. Schemas/fixtures may remain temporarily for legacy tests only.

## 5. Tools and graphs

### 5.1 Shared loop

LangGraph pattern: `call_model` → if tool_calls then `execute_tools` → else `finalize` (JSON schema validate). Every iteration appends one `trace.rounds[]` entry.

### 5.2 Tool catalog

| Tool | Analyst | Portfolio | Behavior |
|------|---------|-----------|----------|
| `get_daily_bars` | ✓ | | Lookback default 20; existing Alpaca/free providers |
| `get_news` | ✓ | | Finnhub company-news; top 3 per symbol |
| `web_search` | ✓ | | **On by default**; see §5.3 |
| `get_account_view` | ✓ | ✓ | Returns injected snapshot (no Trading API call) |
| `get_risk_context` | ✓ | ✓ | Returns injected risk_context |
| `get_last_closes` | | ✓ | Closes for sizing |
| `size_proposals` | | ✓ | Deterministic sizing (existing rules: buy budget / sell fraction / ±10% stops) |

Analyst must not expose `size_proposals`. Portfolio must not expose `get_news` / `web_search`.

### 5.3 Web search

- `WEB_SEARCH_ENABLED` default **`true`**.
- Set `false` to disable the tool binding entirely.
- Provider: `WEB_SEARCH_PROVIDER` default `tavily` (or `serper`); credentials via `WEB_SEARCH_API_KEY` (and provider-specific overrides if needed).
- If enabled but key missing / upstream error: tool returns `ok:false` into trace; **do not** fail the analyst step solely for that.

### 5.4 AnalystGraph

- Goal: gather evidence then emit `analyst_result` for every watchlist symbol.
- Weak evidence → `hold` + low confidence rather than inventing conviction.
- Align output to full watchlist before return.

### 5.5 PortfolioGraph

- Input: analyst `result` + account + risk_context.
- Default: call `size_proposals` for baseline; optional short LLM refine (respect cash, position qty, open orders awareness via `get_account_view`).
- Smaller `max_tool_rounds` (default 3).
- Skip `hold`; never sell more than position qty.

### 5.6 Go chain

```text
created → analyst → portfolio → (risk eval / submit) → executed | awaiting_approval | failed
```

Replace step names `data|research|decision|risk` in status machine and UI labels.

## 6. UI observability

On `/runs/[id]`:

1. Step list uses `analyst` / `portfolio`.
2. Default panel: compact **result** summary (sides/biases; proposals table).
3. **Show tool trace**: chronological rounds with LLM latency and each tool call (name, args, ok, latency, preview, error).
4. Header chips: `stop_reason`, token `usage` when present.
5. **Show raw payload**: full JSON fallback (current behavior).

No new table required for v1.

## 7. Configuration

| Variable | Default / notes |
|----------|-----------------|
| `AGENT_RUNTIME_URL` | Go → runtime base URL |
| `FINNHUB_API_KEY` | News tool |
| `WEB_SEARCH_ENABLED` | **`true`** |
| `WEB_SEARCH_PROVIDER` | `tavily` (or `serper`) |
| `WEB_SEARCH_API_KEY` | Required for successful web_search when enabled |
| `MAX_TOOL_ROUNDS_ANALYST` | `8` |
| `MAX_TOOL_ROUNDS_PORTFOLIO` | `3` |
| `MARKET_DATA_PROVIDER` | `alpaca` / `free` for bars tool |
| `LLM_MODE` / `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` | Existing |

## 8. Migration

1. Add `agent-runtime` with graphs, tools, schemas, healthz.
2. Switch Go runner chain + Alpaca snapshot builder + `risk_context`.
3. Update Compose/env samples; stop scheduling traffic to old five agent services; remove them from main compose.
4. Update contracts + fixtures; migrate tests.
5. Upgrade Runs UI for tool timeline.
6. Sync product-overview, eod-workflow-flowchart, deploy README.

Phasing tip: land runtime + Go chain + mock LLM traces first; then enable Finnhub/web search in deploy env.

## 9. Testing

- Unit: tools with mocked HTTP; graphs under `LLM_MODE=mock` (or scripted tool-call fixtures) produce valid `{result,trace}`.
- Contract: validate `analyst_result`, `portfolio_result`, envelope with `trace.rounds`.
- Go: two-step runner persist; risk/submit regression; open_orders present in request body.
- Web: render mock trace timeline from fixture payload.
- E2E: mock mode without external keys succeeds; optional live Finnhub/web search smoke when keys present.

## 10. Non-goals reminder

OHLCV alone was never enough for portfolio-aware decisions; this design fixes that via **injected account state + tool-loop retrieval**, while keeping **Go + Alpaca** as execution authority.
