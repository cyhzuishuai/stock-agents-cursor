# Strategy Scheduler + Runs Observability — Design Spec

**Date:** 2026-07-28  
**Status:** Accepted  
**Scope:** P1 only (strategies + scheduled runs + run step payloads). Alpaca ledger/execution is **out of scope** (P2).

## 1. Goal

Make trading cadence and auto-execution **configurable via strategies** (DB + API + Settings UI), drive the in-process scheduler from the **single active strategy**, and make each run’s agent/LLM outputs **inspectable** on the Runs detail page.

### 1.1 Success criteria

- Settings shows a Strategies section with CRUD (except delete of system default) and activate-one semantics.
- Seeded system default **整体策略1**: pre-open 10 minutes before US regular open (09:30 ET → 09:20), intraday every 60 minutes from 10:00–15:00 ET, `execution_mode=auto_reject_breaches`.
- Scheduler hot-reloads when the active strategy (or its schedule fields) changes.
- Under `auto_reject_breaches`, risk breaches never create pending human approvals; proposals become `rejected` and the run can still finish as `executed` when nothing is awaiting approval.
- Run detail expands each workflow step and shows parsed `payload_json` (data / research / decision / portfolio / risk agent returns).
- Manual `POST /api/v1/runs/eod` still works; runs record `strategy_id` + `trigger`.

### 1.2 Out of scope (P1)

- Alpaca account, positions, live/paper order routing (P2).
- Per-strategy watchlist or risk-rule overrides.
- Multiple concurrently active strategies.
- Real-time WebSocket updates for runs.
- Changing fill pricing to true intraday quotes (continue using data-agent close / available daily marks).

## 2. Product decisions (locked)

| Topic | Choice |
|-------|--------|
| Approach | Strategy table drives scheduler (not env-only cron) |
| Strategy model | Multi-strategy library; **exactly one** `is_active` at a time |
| Editable fields | name, description, pre-open minutes, intraday interval + ET window, execution mode |
| CRUD | create / edit / delete; system default not deletable; cannot delete active |
| Breach handling | `auto_reject_breaches` (default) vs `require_approval` (legacy behavior) |
| Fill price | Existing data-agent marks (close-based approximation) |
| Follow-up | Full Alpaca ledger/execution = separate P2 project |

## 3. Data model

### 3.1 `strategies`

| Column | Type | Notes |
|--------|------|-------|
| `id` | uint PK | |
| `name` | string | required, unique recommended |
| `description` | text | optional |
| `is_system_default` | bool | seeded true for 整体策略1; immutable delete |
| `is_active` | bool | at most one true globally |
| `pre_open_minutes` | int | minutes before 09:30 ET; `0` disables pre-open run; default `10` |
| `intraday_every_minutes` | int | `0` disables intraday; default `60`; if >0 must be ≥15 |
| `intraday_start_et` | string | `"HH:MM"` ET; default `"10:00"` |
| `intraday_end_et` | string | `"HH:MM"` ET inclusive of matching ticks; default `"15:00"` |
| `execution_mode` | string | `auto_reject_breaches` \| `require_approval` |
| `created_at` / `updated_at` | timestamp | |

**Seed row — 整体策略1**

- `is_system_default=true`, `is_active=true`
- `pre_open_minutes=10`, `intraday_every_minutes=60`, window `10:00`–`15:00` ET
- `execution_mode=auto_reject_breaches`
- Description states: system default; auto-executes within risk limits; rejects breaches without human approval.

### 3.2 `workflow_runs` additions

| Column | Type | Notes |
|--------|------|-------|
| `strategy_id` | *uint nullable FK | null for legacy runs |
| `trigger` | string | `manual` \| `pre_open` \| `intraday` \| `legacy_eod` |

Existing `workflow_step_results.payload_json` remains the store for full agent responses; no schema change required for observability.

### 3.3 Unchanged globals

Watchlist and `risk_rule_configs` stay account/global Settings. Strategies do not override them in P1.

## 4. API

All strategy endpoints require JWT (same as other `/api/v1/*` routes).

| Method | Path | Behavior |
|--------|------|----------|
| `GET` | `/api/v1/strategies` | List all strategies |
| `GET` | `/api/v1/strategies/:id` | Detail |
| `POST` | `/api/v1/strategies` | Create (`is_active=false`, `is_system_default=false`) |
| `PATCH` | `/api/v1/strategies/:id` | Update allowed fields; if updating the active strategy’s schedule fields → reload scheduler |
| `POST` | `/api/v1/strategies/:id/activate` | Transaction: set this active, clear others; reload scheduler |
| `DELETE` | `/api/v1/strategies/:id` | 403 if `is_system_default` or `is_active` |

**Validation**

- `execution_mode` ∈ enum
- `pre_open_minutes` ≥ 0
- `intraday_every_minutes` = 0 or ≥ 15
- `intraday_start_et` / `intraday_end_et` parse as `HH:MM`; start ≤ end
- Regular open assumed fixed at **09:30 America/New_York** for pre-open offset (P1; no early-close calendar)

**Runs**

- `POST /api/v1/runs/eod` → `trigger=manual`, attach current active `strategy_id` (if any)
- `GET /api/v1/runs` and `GET /api/v1/runs/:id` include `strategy_id`, `strategy_name` (join/lookup), `trigger`
- Detail `steps` must include `payload_json` (already on model; do not strip)

## 5. Scheduler & execution

### 5.1 Schedule derivation

From active strategy (Mon–Fri, `America/New_York`):

1. **Pre-open** (if `pre_open_minutes` > 0): fire at `09:30 − pre_open_minutes` (e.g. 09:20).
2. **Intraday** (if `intraday_every_minutes` > 0): ticks from `intraday_start_et` through `intraday_end_et` stepping by interval (e.g. 10:00, 11:00, …, 15:00).

Implementation: replace single env-primary cron with jobs registered from the active strategy. On activate / schedule PATCH of active strategy: stop previous jobs and register new ones (**hot reload**).

`EOD_CRON`: optional fallback only when no active strategy exists; docs state DB strategy is authoritative.

### 5.2 Concurrency

- Do not run two workflow executions in parallel for the same account.
- Prefer a Redis (or in-process) lock keyed by run intent (e.g. `trade_date` + trigger slot, or a global “runner busy” lock). If a tick arrives while a run is in progress, skip or queue—**skip with log** is acceptable for P1.
- Multiple runs per `trade_date` are allowed (pre-open + hourly).

### 5.3 Execution mode in runner

For each proposal after Go risk `Evaluate`:

| Mode | AutoExecute true | AutoExecute false |
|------|------------------|-------------------|
| `require_approval` | Paper fill (existing) | Create `approvals` row; proposal `awaiting_approval`; run may end `awaiting_approval` |
| `auto_reject_breaches` | Paper fill | **No** pending approval; proposal → `rejected`; store breach reasons on proposal (`breach_reasons_json`); Approvals list stays free of new pending items |

Run terminal status: if any proposal still `awaiting_approval` → `awaiting_approval`; else → `executed` (including when some were `rejected`). Agent/infra failures still → `failed` with no ledger writes for that failed path (existing semantics).

Mode is read from the strategy linked on the run (snapshot at start); if `strategy_id` null, default to `require_approval` for backward compatibility.

### 5.4 Trigger labels on created runs

| Source | `trigger` |
|--------|-----------|
| Manual UI / `POST .../runs/eod` | `manual` |
| Pre-open cron | `pre_open` |
| Intraday cron | `intraday` |
| Legacy env-only EOD fallback | `legacy_eod` |

## 6. Frontend

### 6.1 Settings → Strategies

Add a **Strategies** panel on the existing Settings page (no new top-nav item required):

- Table: name, system-default badge, schedule summary, execution mode, Active
- Actions: Activate, Edit, Delete (hidden/disabled for system default / active), Create
- Form fields match §3.1 editable columns
- Reuse existing `panel` / `data-table` / `btn` styles

### 6.2 Runs list

Show `trigger` and strategy name when present.

### 6.3 Runs detail

- Header: status, trade date, trigger, strategy name
- Steps: expandable rows; expanded body = pretty-printed JSON from `payload_json` (raw string fallback on parse error)
- Proposals/Orders tables unchanged; `rejected` visible
- Default collapsed payloads; click to expand (avoid huge default DOM)

## 7. Testing

- Strategy CRUD + activate uniqueness (API/service tests)
- Scheduler derivation: 10-min pre-open + hourly window generates expected ET times; reload replaces jobs
- Runner: `auto_reject_breaches` creates no pending approvals; proposal `rejected`; run `executed`
- Runner: `require_approval` still creates approvals (regression)
- Web: Settings strategies panel smoke; Run detail renders payload from fixture `payload_json`

## 8. Error handling

- Invalid strategy payloads → 400 with clear field errors
- Delete/activate conflicts → 403/409 as appropriate
- Scheduler reload failure → log loudly; keep previous schedule if reload aborts mid-way (best-effort: reload in critical section)
- Missing active strategy → log and **register no automatic ticks** (seed guarantees one active on fresh install). `EOD_CRON` is legacy-only for environments that have not migrated; new deploys rely on strategies.

## 9. Relationship to existing docs

- Extends cadence beyond single EOD 16:30 described in `docs/eod-workflow-flowchart.md` and the 2026-07-23 design spec.
- Does not replace paper ledger ownership in Go for P1.
- P2 (Alpaca as source of truth for cash/positions/orders) remains a separate design.

## 10. Implementation defaults (plan may refine mechanics, not product intent)

1. **Auto-reject audit:** add optional `breach_reasons_json` on `trade_proposals` when status becomes `rejected` under `auto_reject_breaches` (do not create pending Approvals UI rows).
2. **Locking:** global “workflow runner busy” Redis lock for P1; overlapping ticks skip with log.
3. **PATCH reload:** only reload scheduler when the patched row is currently `is_active` or after `activate`.
