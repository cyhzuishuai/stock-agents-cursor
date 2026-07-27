# US Stock Paper-Trading Multi-Agent System — Design Spec

**Date:** 2026-07-23  
**Status:** Historical V1 baseline (partially superseded)  
**Version:** V1 (paper trading only)

> **Supersessions (read these first for current product behavior):**  
> - **Cadence / scheduling / runs observability:** `2026-07-28-strategy-scheduler-runs-observability-design.md` — product is **strategy-driven** (pre-open + intraday), not single EOD cron.  
> - **Cash / positions / orders authority:** `2026-07-28-alpaca-paper-authority-design.md` — Alpaca Paper is source of truth (local ledger no longer authoritative).  
> - **Watchlist / risk Settings edit:** `2026-07-28-settings-watchlist-risk-edit-design.md`.

## 1. Goal

Build a self-hosted **US equities paper-trading** system where multiple specialized agents produce trade proposals on a schedule, a Go service applies **deterministic risk rules**, and human approval is required only when thresholds are breached. The UI provides a portfolio-centric overview (NAV, cash, positions, runs, approvals).

> V1 text below originally assumed a single post-close EOD run; **shipping cadence is strategy-driven** (see supersession note above).

### 1.1 Success criteria (V1)

- Fixed watchlist (10–30 symbols) runs once per US trading day after close (plus manual trigger for development).
- Five Python agents produce structured outputs; only the Go API mutates the paper ledger.
- Portfolio management is first-class: cash, positions, weights, stop-loss / take-profit fields, concentration, simple drawdown from NAV highs.
- Trades within risk limits auto-execute on the paper account; over-limit proposals require per-order human approval.
- Entire stack runs via Docker Compose (web, api, agents, PostgreSQL, Redis).

### 1.2 Out of scope (V1)

- Live brokerage execution
- Multi-tenant / multi-role auth
- Intraday trading or partial fills
- Broad market scanning / universe selection UI
- Options, margin, leverage
- Full backtest engine or workflow visual editor
- Complex tax/lot accounting

## 2. Product decisions (locked)

| Topic | Choice |
|-------|--------|
| Focus | Portfolio management (not signal-only or strategy lab) |
| Backend | Hybrid: Go (Gin + Gorm) for API, ledger, workflow, approval; Python containers for agents |
| Frontend | Next.js App Router |
| Agents | Data → Research → Decision → Portfolio → Risk |
| Approval | Deterministic rule thresholds; auto vs `awaiting_approval` |
| Cadence | ~~EOD (US/Eastern after close)~~ **Superseded:** strategy pre-open + intraday — see `2026-07-28-strategy-scheduler-runs-observability-design.md` |
| Market data | Adapter layer; **implement free provider first**; default config path toward Alpaca Market Data; `MARKET_DATA_PROVIDER=free\|alpaca` |
| Agent intelligence | LLM + structured JSON for research/decision/(portfolio assist); risk gates are rules in Go |
| Users | Single admin user (JWT) |
| Watchlist | Fixed list in config/DB seed; no V1 UI editing |

## 3. Architecture

### 3.1 Services (Docker Compose)

| Service | Stack | Responsibility |
|---------|-------|----------------|
| `web` | Next.js | Login, overview, portfolio, runs, approvals, read-only settings |
| `api` | Go + Gin + Gorm | Auth, paper ledger, risk evaluation, EOD orchestration, approval APIs |
| `agent-data` | Python | Fetch daily bars via market-data adapter |
| `agent-research` | Python + LLM | Per-symbol thesis → structured JSON |
| `agent-decision` | Python + LLM | Trade intents (side/urgency/rationale) → structured JSON |
| `agent-portfolio` | Python + LLM/rules | Sizing, target weights, stops, cash constraints → executable proposals |
| `agent-risk` | Python | Flags/scores and advisory `auto\|review` (non-binding) |
| `postgres` | PostgreSQL | System of record |
| `redis` | Redis | EOD run lock, short-lived market-data cache, optional job queue |

### 3.2 Trust boundaries

- Agents **never** write cash, positions, or orders.
- Only `api` commits ledger changes after `auto_execute` or approval `approved`.
- Risk Agent output is **advisory**; Go rule engine is authoritative.
- LLM outputs must validate against JSON schemas before the workflow advances.

### 3.3 EOD data flow

1. Scheduler (in-api cron or internal HTTP) acquires Redis lock for `trade_date`.
2. `api` creates `workflow_run` and loads account snapshot + watchlist.
3. Sequentially calls agents: Data → Research → Decision → Portfolio → Risk.
4. Persists each step result on `workflow_step_results`.
5. Go evaluates each `trade_proposal` against `risk_rule_configs`.
6. Within limits → paper fill at EOD close; over limits → `approvals` rows + run may be `awaiting_approval` (partial auto-exec allowed for non-breaching orders).
7. Writes `nav_snapshots` for the day when the run reaches a terminal accounting state (all proposals executed, rejected, or cancelled).

```text
[cron/manual] -> api(lock) -> workflow_run
    -> agent-data -> agent-research -> agent-decision
    -> agent-portfolio -> agent-risk
    -> Go rules -> auto fills and/or approvals
    -> nav_snapshot
```

## 4. Agent contracts

### 4.1 Transport

- Each agent exposes `POST /v1/run`.
- Request includes: `run_id`, `trade_date`, `watchlist`, `account_snapshot`, `prior_step_outputs` (as needed).
- Response: structured JSON + optional `warnings[]`.
- Timeouts and limited retries (e.g. 2) on 5xx/timeout; schema failure retries once; then step → failed, run → `failed`, **no ledger writes**.

### 4.2 Outputs (normative fields)

**Data**

- Per symbol: OHLCV (daily), optional derived metrics (return, volatility).

**Research**

- Per symbol: `bias` (`bull` \| `bear` \| `neutral`), `confidence` (0–1), `thesis` (short text).

**Decision**

- List of intents: `symbol`, `side` (`buy` \| `sell` \| `hold`), `urgency`, `rationale`.
- No final quantity required at this stage.

**Portfolio**

- Executable proposals: `symbol`, `side`, `qty` and/or `target_weight`, `stop_loss`, `take_profit`, `estimated_notional`, `estimated_cash_impact`.

**Risk (advisory)**

- `flags[]`, numeric `scores`, suggested action `auto` \| `review` per proposal.
- Does not override Go thresholds.

### 4.3 Workflow states

Step progression: `created` → `data` → `research` → `decision` → `portfolio` → `risk`.

After risk + Go rules:

| Run status | Meaning |
|------------|---------|
| `awaiting_approval` | At least one proposal still needs human decision (other proposals may already be filled) |
| `executed` | No pending approvals; every proposal is filled, rejected, or cancelled |
| `failed` | Agent/infra/schema failure during the agent chain; **no ledger writes** for that run |
| `cancelled` | User cancelled; pending proposals cancelled; any fills already committed in this run remain |

Notes:

- `rejected` is an **approval/proposal** status, not a run status.
- Per-order approval (not batch-only) in V1.
- Auto-fill eligible proposals immediately after Go rules; do not wait on unrelated approvals.

## 5. Risk rules (authoritative in Go)

Configurable thresholds (DB seeded from env), examples:

- Max notional per order
- Max single-name weight after trade
- Min cash ratio after trade
- Max portfolio concentration (e.g. top-N weight)
- Optional max expected drawdown proxy vs recent NAV peak

Evaluation:

- All checked rules pass → auto paper execute.
- Any fail → create `approval` with `breach_reasons[]`; block that proposal until approved/rejected.
- Risk Agent suggestion is stored for audit only.

## 6. Paper ledger

> **Superseded (cash / positions / orders authority):** Phase 1 shipped under `docs/superpowers/specs/2026-07-28-alpaca-paper-authority-design.md`. Alpaca Paper is now the system of record for account cash, positions, orders, and fill prices; Go submits market orders instead of local `ApplyFill`. The sections below describe the original V1 local-ledger model retained for historical context and offline/mocked-broker tests.

### 6.1 Accounting rules

- Currency: USD.
- EOD fill price = that symbol’s close for `trade_date` (or last available close if explicitly allowed with warning).
- Buy: require sufficient cash; increase/create position; update `avg_cost`.
- Sell: require sufficient qty; increase cash; reduce/close position.
- Fees: V1 constant `0` or fixed bps via config (default `0`).
- Stop-loss / take-profit: stored on position/proposal for display and future use; **V1 does not auto-trigger intraday stops** (EOD-only system). They may inform the next day’s portfolio agent via account snapshot.

### 6.2 Tables

| Table | Purpose |
|-------|---------|
| `users` | Single admin (password hash) |
| `accounts` | Initial capital, cash balance |
| `positions` | symbol, qty, avg_cost, optional stop/take |
| `orders` | Paper orders linked to run / approval |
| `watchlist_symbols` | Fixed universe |
| `workflow_runs` | Run header + status |
| `workflow_step_results` | Raw JSON per step |
| `trade_proposals` | Portfolio agent drafts |
| `approvals` | Human decisions + breach reasons |
| `risk_rule_configs` | Threshold parameters |
| `nav_snapshots` | Daily NAV = cash + MTM positions |

## 7. Frontend

| Route | Purpose |
|-------|---------|
| `/login` | Admin login |
| `/` | Overview: NAV, cash, pending approvals, latest run, position summary, NAV sparkline |
| `/portfolio` | Positions, weights, P&L, stops |
| `/runs` | Run history |
| `/runs/[id]` | Step timeline, proposals, outcomes |
| `/approvals` | Approve/reject with note |
| `/settings` | Read-only watchlist, rules, data provider |

Auth: JWT from `api`. Compose reverse-proxy or browser calls `api` with CORS configured for `web`.

Manual “Run EOD now” control for development; production relies on schedule.

## 8. Market data adapter

- Interface in shared Python package used by `agent-data` (and reusable by tests).
- Providers:
  - `free`: Yahoo Finance or equivalent daily bars (V1 implementation priority).
  - `alpaca`: Alpaca Market Data (wired as default *target* provider name in prod config once keys exist).
- `api` does not scrape markets directly; it consumes Data agent output (and may cache bars in Redis/DB if useful for NAV MTM).

## 9. Error handling

- Redis lock prevents concurrent EOD runs for the same `trade_date`.
- Agent/LLM failures fail the run without ledger mutation.
- Missing data for one symbol → skip symbol with warning; all symbols missing → run `failed`.
- Approvals do not auto-expire in V1; user may cancel the run/proposals.
- Each approval applies independently. Upsert `nav_snapshots` for `trade_date` after every successful fill batch and again when the run becomes terminal (`executed` or `cancelled`).

## 10. Testing strategy

- **Go unit tests:** ledger invariants, risk gate matrix, workflow transitions.
- **Python tests:** JSON schema validation; `free` provider with mocked HTTP.
- **API integration (lightweight):** create run with stubbed agent HTTP → auto-exec path; approval path.
- **Frontend:** smoke tests for overview/approvals; no mandatory full E2E in V1.

## 11. Deployment

- Single `docker-compose.yml` (optional `docker-compose.override.yml` for local free data + mock LLM).
- Persistent volume for Postgres.
- Secrets via env: `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`, `LLM_API_KEY`, `ALPACA_*` (optional), `MARKET_DATA_PROVIDER`, risk seeds, watchlist seed, admin bootstrap password.
- Network: `api` reaches agents by Compose DNS names.
- Schedule: US/Eastern post-close cron inside `api` or `POST /internal/eod/run` protected by internal token.

## 12. Repository layout (planned)

```text
/
  apps/web/                 # Next.js
  services/api/             # Go Gin API
  services/agents/
    data/
    research/
    decision/
    portfolio/
    risk/
    common/                 # schemas, adapter, HTTP helpers
  deploy/docker-compose.yml
  docs/superpowers/specs/
```

Exact package names may adjust during implementation planning; boundaries above are normative.

## 13. Open parameters (explicit defaults, not TBD)

These are configurable defaults for implementation, not unresolved design questions:

| Parameter | V1 default |
|-----------|------------|
| Initial cash | `100000` USD |
| Watchlist size | 10–30 symbols (seed file) |
| Max order notional | `10000` USD |
| Max single-name weight | `20%` |
| Min cash ratio | `10%` |
| Agent HTTP timeout | `120s` (LLM steps) |
| Data agent timeout | `60s` |
| Fee rate | `0` |
| Fill price | EOD close |

## 14. Approach rationale

Chosen orchestration: **Go state machine + HTTP agent calls** over Temporal or Python-centric Celery. Fits single-user EOD paper trading, keeps ledger/approval ownership in Go, and maps cleanly to Docker services without a heavyweight workflow engine in V1.
