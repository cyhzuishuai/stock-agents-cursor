# Plan 01 — Scaffold & Shared Contracts

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or executing-plans.
>
> **Wave:** 0 (SERIAL — blocks all other plans)  
> **Track:** T-CONTRACTS (+ minimal dirs for other tracks)  
> **Parallel:** none

**Goal:** Create monorepo skeleton and freeze JSON contracts so parallel subagents share identical field names.

**Architecture:** Contracts live in `packages/contracts/`; empty service directories are placeholders only.

**Tech Stack:** JSON Schema draft-07, Markdown DTO list, git.

## Global Constraints

See `2026-07-23-paper-trading-00-master.md`. Do not implement business logic here.

---

### Task 01.1: Repository directory skeleton

**Files:**
- Create: `apps/web/.gitkeep`
- Create: `services/api/.gitkeep`
- Create: `services/agents/common/.gitkeep`
- Create: `services/agents/data/.gitkeep`
- Create: `services/agents/research/.gitkeep`
- Create: `services/agents/decision/.gitkeep`
- Create: `services/agents/portfolio/.gitkeep`
- Create: `services/agents/risk/.gitkeep`
- Create: `packages/contracts/.gitkeep`
- Create: `deploy/.gitkeep`
- Create: `README.md`

**Interfaces:**
- Consumes: none
- Produces: directory layout matching spec §12

- [ ] **Step 1: Create directories and README**

```markdown
# stock-agents-cursor

US equities EOD paper-trading multi-agent system (V1).

See `docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md`.
Implementation plans: `docs/superpowers/plans/`.
```

- [ ] **Step 2: Verify tree**

Run: `Get-ChildItem -Recurse -Directory services,apps,packages,deploy | Select-Object FullName`  
Expected: all paths listed above exist

- [ ] **Step 3: Commit**

```bash
git add apps services packages deploy README.md
git commit -m "chore: scaffold monorepo directories"
```

---

### Task 01.2: Agent run request schema

**Files:**
- Create: `packages/contracts/agent_run_request.schema.json`
- Test: `packages/contracts/fixtures/agent_run_request.valid.json`
- Test: `packages/contracts/scripts/validate_fixtures.py` (minimal)

**Interfaces:**
- Consumes: none
- Produces: schema for `POST /v1/run` body used by all agents and Go `agentsclient`

- [ ] **Step 1: Write valid fixture**

```json
{
  "run_id": "11111111-1111-1111-1111-111111111111",
  "trade_date": "2026-07-22",
  "watchlist": ["AAPL", "MSFT"],
  "account_snapshot": {
    "cash": 100000.0,
    "currency": "USD",
    "positions": [
      {
        "symbol": "AAPL",
        "qty": 10,
        "avg_cost": 180.0,
        "stop_loss": 160.0,
        "take_profit": 220.0
      }
    ]
  },
  "prior_step_outputs": {}
}
```

- [ ] **Step 2: Write JSON Schema**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "agent_run_request.schema.json",
  "type": "object",
  "required": ["run_id", "trade_date", "watchlist", "account_snapshot"],
  "properties": {
    "run_id": { "type": "string", "format": "uuid" },
    "trade_date": { "type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$" },
    "watchlist": {
      "type": "array",
      "minItems": 1,
      "items": { "type": "string", "minLength": 1 }
    },
    "account_snapshot": {
      "type": "object",
      "required": ["cash", "currency", "positions"],
      "properties": {
        "cash": { "type": "number" },
        "currency": { "type": "string", "const": "USD" },
        "positions": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["symbol", "qty", "avg_cost"],
            "properties": {
              "symbol": { "type": "string" },
              "qty": { "type": "number" },
              "avg_cost": { "type": "number" },
              "stop_loss": { "type": ["number", "null"] },
              "take_profit": { "type": ["number", "null"] }
            }
          }
        }
      }
    },
    "prior_step_outputs": { "type": "object" }
  },
  "additionalProperties": false
}
```

- [ ] **Step 3: Add validator script + run it**

```python
# packages/contracts/scripts/validate_fixtures.py
import json, sys
from pathlib import Path
try:
    import jsonschema
except ImportError:
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "jsonschema"])
    import jsonschema

root = Path(__file__).resolve().parents[1]
schema = json.loads((root / "agent_run_request.schema.json").read_text())
fixture = json.loads((root / "fixtures" / "agent_run_request.valid.json").read_text())
jsonschema.validate(fixture, schema)
print("OK agent_run_request")
```

Run: `python packages/contracts/scripts/validate_fixtures.py`  
Expected: `OK agent_run_request`

- [ ] **Step 4: Commit**

```bash
git add packages/contracts
git commit -m "feat: add agent_run_request JSON schema"
```

---

### Task 01.3: Data + Research result schemas

**Files:**
- Create: `packages/contracts/data_result.schema.json`
- Create: `packages/contracts/research_result.schema.json`
- Create: `packages/contracts/fixtures/data_result.valid.json`
- Create: `packages/contracts/fixtures/research_result.valid.json`
- Modify: `packages/contracts/scripts/validate_fixtures.py`

**Interfaces:**
- Produces: Data agent & Research agent response shapes

- [ ] **Step 1: Fixtures**

`data_result.valid.json`:

```json
{
  "bars": [
    {
      "symbol": "AAPL",
      "trade_date": "2026-07-22",
      "open": 190.0,
      "high": 192.0,
      "low": 188.0,
      "close": 191.0,
      "volume": 1000000,
      "return_1d": 0.01,
      "volatility_20d": 0.02
    }
  ],
  "warnings": []
}
```

`research_result.valid.json`:

```json
{
  "items": [
    {
      "symbol": "AAPL",
      "bias": "bull",
      "confidence": 0.72,
      "thesis": "Momentum and volume supportive."
    }
  ],
  "warnings": []
}
```

- [ ] **Step 2: Schemas**

`data_result.schema.json` — required `bars` array; each bar requires `symbol`, `trade_date`, `open`, `high`, `low`, `close`, `volume`; optional `return_1d`, `volatility_20d`; optional `warnings` string array.

`research_result.schema.json` — required `items`; each item requires `symbol`, `bias` enum [`bull`,`bear`,`neutral`], `confidence` 0–1, `thesis` string; optional `warnings`.

- [ ] **Step 3: Extend validator to load all `*.schema.json` with matching `fixtures/*.valid.json`**

Run: `python packages/contracts/scripts/validate_fixtures.py`  
Expected: prints OK for each pair

- [ ] **Step 4: Commit**

```bash
git commit -am "feat: add data and research result schemas"
```

---

### Task 01.4: Decision + Portfolio + Risk advisory schemas

**Files:**
- Create: `packages/contracts/decision_result.schema.json`
- Create: `packages/contracts/portfolio_result.schema.json`
- Create: `packages/contracts/risk_advisory_result.schema.json`
- Create: matching `fixtures/*.valid.json`
- Modify: validator

**Interfaces:**
- Produces: Decision / Portfolio / Risk-agent response shapes consumed by Go workflow

- [ ] **Step 1: Fixtures**

Decision:

```json
{
  "intents": [
    {
      "symbol": "AAPL",
      "side": "buy",
      "urgency": "normal",
      "rationale": "Bullish research with capacity."
    }
  ],
  "warnings": []
}
```

Portfolio:

```json
{
  "proposals": [
    {
      "symbol": "AAPL",
      "side": "buy",
      "qty": 20,
      "target_weight": 0.15,
      "stop_loss": 170.0,
      "take_profit": 230.0,
      "estimated_notional": 3820.0,
      "estimated_cash_impact": -3820.0
    }
  ],
  "warnings": []
}
```

Risk advisory:

```json
{
  "items": [
    {
      "symbol": "AAPL",
      "side": "buy",
      "flags": ["size_ok"],
      "scores": { "liquidity": 0.9, "volatility": 0.4 },
      "suggested_action": "auto"
    }
  ],
  "warnings": []
}
```

- [ ] **Step 2: Write schemas enforcing enums**

- `side`: `buy` | `sell` | `hold` (decision); portfolio proposals `buy` | `sell` only  
- `suggested_action`: `auto` | `review`  
- Portfolio proposal requires `symbol`, `side`, `qty`, `estimated_notional`, `estimated_cash_impact`; optional stops/weight

- [ ] **Step 3: Validate all fixtures**

Run: `python packages/contracts/scripts/validate_fixtures.py`  
Expected: all OK

- [ ] **Step 4: Commit**

```bash
git commit -am "feat: add decision portfolio risk advisory schemas"
```

---

### Task 01.5: API DTO sketch + env example keys

**Files:**
- Create: `packages/contracts/api_dto.md`
- Create: `deploy/env.example`

**Interfaces:**
- Produces: REST field names for Plan 08 frontend and Plan 02/07 handlers

- [ ] **Step 1: Write `api_dto.md`** with these endpoints and JSON fields (no implementation):

```markdown
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

## Settings (read-only)
GET /api/v1/settings -> { watchlist: string[], risk_rules: object, market_data_provider: string }

## Internal
POST /internal/eod/run  Header X-Internal-Token -> { run_id }
```

- [ ] **Step 2: Write `deploy/env.example`**

```bash
DATABASE_URL=postgres://stock:stock@postgres:5432/stock?sslmode=disable
REDIS_URL=redis://redis:6379/0
JWT_SECRET=change-me
ADMIN_USERNAME=admin
ADMIN_PASSWORD=admin123
INITIAL_CASH=100000
MARKET_DATA_PROVIDER=free
LLM_API_KEY=
LLM_BASE_URL=
ALPACA_API_KEY=
ALPACA_API_SECRET=
INTERNAL_EOD_TOKEN=dev-internal-token
WATCHLIST=AAPL,MSFT,GOOGL,AMZN,META,NVDA,TSLA,JPM,V,UNH
RISK_MAX_ORDER_NOTIONAL=10000
RISK_MAX_SINGLE_NAME_WEIGHT=0.20
RISK_MIN_CASH_RATIO=0.10
AGENT_DATA_URL=http://agent-data:8001
AGENT_RESEARCH_URL=http://agent-research:8002
AGENT_DECISION_URL=http://agent-decision:8003
AGENT_PORTFOLIO_URL=http://agent-portfolio:8004
AGENT_RISK_URL=http://agent-risk:8005
```

- [ ] **Step 3: Commit**

```bash
git add packages/contracts/api_dto.md deploy/env.example
git commit -m "feat: freeze API DTO sketch and env example"
```

---

### Task 01.6: Wave-0 gate checklist

**Files:** none (verification only)

- [ ] **Step 1: Confirm all schemas validate**

Run: `python packages/contracts/scripts/validate_fixtures.py`

- [ ] **Step 2: Confirm no Go/Python/Web business code yet** (only contracts + empty dirs + README + env.example)

- [ ] **Step 3: Announce Wave 0 complete** so orchestrator may start Plan 02, Plan 05 (common tasks only), Plan 08 (scaffold) in parallel
