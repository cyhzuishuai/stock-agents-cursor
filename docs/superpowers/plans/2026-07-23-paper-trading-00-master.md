# US Stock Paper-Trading — Master Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.
>
> **Parallel rule (user mandate):** Tasks are intentionally fine-grained. Assign **one subagent per task** (or one per Parallel Group within a wave). Never let two subagents edit the same file path. Respect **Depends on** edges.

**Goal:** Deliver V1 US equities EOD paper-trading multi-agent system (Go API + 5 Python agents + Next.js + Docker) per `docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md`.

**Architecture:** Go Gin orchestrates EOD workflow and owns the paper ledger; Python agent containers expose `POST /v1/run`; Next.js shows overview/approvals; Compose runs postgres+redis+all services.

**Tech Stack:** Go 1.22+, Gin, Gorm, PostgreSQL, Redis, Python 3.12, FastAPI (agents), Next.js 15 App Router, Docker Compose, JWT auth.

## Global Constraints

- Spec path: `docs/superpowers/specs/2026-07-23-us-stock-paper-trading-agents-design.md` (normative).
- Initial cash: `100000` USD; fee rate: `0`; fill price: EOD close.
- Risk defaults: max notional `10000`, max single-name weight `20%`, min cash ratio `10%`.
- Timeouts: data agent `60s`, LLM agents `120s`; agent retries: 2 on 5xx/timeout; schema retry: 1.
- `MARKET_DATA_PROVIDER=free|alpaca` — implement **free** first; Alpaca stub OK until keys exist.
- Agents never write ledger; only `services/api` mutates cash/positions/orders.
- Go risk rules are authoritative; Python risk agent is advisory only.
- Single admin user + JWT; fixed watchlist (no UI edit in V1).
- **File ownership:** each Parallel Track owns disjoint paths (see below). Shared contracts only change in Wave 0 / Plan 01.
- **Commits:** one commit per task; message prefix `feat:`, `test:`, `chore:`, or `fix:`.

## Sub-plans (execute by wave)

| Plan file | Track | Wave |
|-----------|-------|------|
| [01-scaffold-contracts](./2026-07-23-paper-trading-01-scaffold-contracts.md) | contracts + repo skeleton | 0 (serial) |
| [02-go-auth-models](./2026-07-23-paper-trading-02-go-auth-models.md) | Go auth + Gorm models + seed | 1 |
| [03-go-ledger](./2026-07-23-paper-trading-03-go-ledger.md) | Paper ledger | 2 |
| [04-go-risk](./2026-07-23-paper-trading-04-go-risk.md) | Deterministic risk engine | 2 |
| [05-python-common-data](./2026-07-23-paper-trading-05-python-common-data.md) | Python common + data agent | 1–2 |
| [06-python-llm-agents](./2026-07-23-paper-trading-06-python-llm-agents.md) | research/decision/portfolio/risk agents | 2–3 |
| [07-go-workflow](./2026-07-23-paper-trading-07-go-workflow.md) | EOD orchestration + approvals API | 3 |
| [08-frontend](./2026-07-23-paper-trading-08-frontend.md) | Next.js UI | 1–3 |
| [09-docker-integration](./2026-07-23-paper-trading-09-docker-integration.md) | Compose + wire-up + smoke | 4 (serial-ish) |

## File ownership (do not cross)

| Track ID | Exclusive paths |
|----------|-----------------|
| T-CONTRACTS | `packages/contracts/**`, `deploy/env.example` (watchlist/risk seed keys only in 01) |
| T-GO-CORE | `services/api/cmd/**`, `services/api/internal/config/**`, `services/api/internal/db/**`, `services/api/internal/models/**`, `services/api/internal/auth/**`, `services/api/internal/httpserver/**` |
| T-GO-LEDGER | `services/api/internal/ledger/**` |
| T-GO-RISK | `services/api/internal/risk/**` |
| T-GO-WF | `services/api/internal/workflow/**`, `services/api/internal/agentsclient/**`, `services/api/internal/approvals/**`, `services/api/internal/scheduler/**` |
| T-PY-COMMON | `services/agents/common/**` |
| T-PY-DATA | `services/agents/data/**` |
| T-PY-RESEARCH | `services/agents/research/**` |
| T-PY-DECISION | `services/agents/decision/**` |
| T-PY-PORTFOLIO | `services/agents/portfolio/**` |
| T-PY-RISK | `services/agents/risk/**` |
| T-WEB | `apps/web/**` |
| T-DEPLOY | `deploy/**` (after Wave 0 env.example exists; Compose full file in Plan 09) |

**Integration seams (read-only across tracks):**

- Go imports JSON field names from `packages/contracts/*.schema.json` (codegen optional; V1: hand-mirror structs with identical tags).
- Python loads the same schemas via `services/agents/common/schemas.py`.
- Frontend types mirror `packages/contracts/openapi-sketch.md` / TS types generated or hand-written in `apps/web/src/lib/types.ts` only after contracts freeze.

## Parallel waves (DAG)

```text
Wave 0  [SERIAL]  Plan 01 — scaffold + JSON schemas locked
    |
    +------------------+------------------+------------------+
    v                  v                  v                  v
Wave 1              Wave 1             Wave 1             Wave 1
Plan 02 Go auth     Plan 05a common    Plan 08a web       (wait)
+ models            package            scaffold
    |                  |
    +--------+---------+--------+---------------------------+
    v        v                  v                           v
Wave 2    Wave 2             Wave 2                      Wave 2
Plan 03   Plan 04            Plan 05b data agent         Plan 06
ledger    risk engine        + free provider             4 LLM agents
                                                         (4-way parallel)
    |        |                  |                           |
    +--------+------------------+---------------------------+
                           v
Wave 3              Wave 3 (parallel with WF after stubs)
Plan 07 workflow    Plan 08b–d frontend pages
+ approvals API     (can use MSW/mock until API ready)
                           v
Wave 4  [mostly SERIAL] Plan 09 docker compose + e2e smoke
```

### Max parallel subagents by wave

| Wave | Suggested concurrent subagents |
|------|--------------------------------|
| 0 | 1 |
| 1 | 3 (Go auth/models, Python common, Web scaffold) |
| 2 | up to 7 (ledger, risk, data agent, research, decision, portfolio, risk-agent) + web pages if mocked |
| 3 | 2–4 (workflow, frontend feature pages) |
| 4 | 1–2 (compose, smoke fixes) |

## Locked shared contract summary (Wave 0 produces these files)

- `packages/contracts/agent_run_request.schema.json`
- `packages/contracts/data_result.schema.json`
- `packages/contracts/research_result.schema.json`
- `packages/contracts/decision_result.schema.json`
- `packages/contracts/portfolio_result.schema.json`
- `packages/contracts/risk_advisory_result.schema.json`
- `packages/contracts/api_dto.md` — REST DTO field list for web↔api

**Canonical agent endpoint:** `POST /v1/run` on every agent service.

**Canonical Go internal package import path:** `github.com/cyh/stock-agents/services/api/...`  
(If module path differs in Plan 01, all Go plans must use the module path written in `services/api/go.mod`.)

## Execution checklist for orchestrator

- [ ] Finish Plan 01 completely before launching Wave 1.
- [ ] Launch Wave 1 subagents in parallel (02, 05-common-only tasks, 08-scaffold).
- [ ] After 02 + 05-common done: launch Wave 2 (03, 04, 05-data, 06×4).
- [ ] After 03+04+05-data+06 done: launch Plan 07; continue Plan 08 pages in parallel with mocks or live API.
- [ ] Plan 09 last: compose, healthchecks, one manual EOD smoke.

## Spec coverage map

| Spec section | Plan |
|--------------|------|
| §3 Architecture / Compose services | 01, 09 |
| §4 Agent contracts | 01, 05, 06 |
| §5 Risk rules | 04, 07 |
| §6 Paper ledger / tables | 02, 03 |
| §7 Frontend | 08 |
| §8 Market data adapter | 05 |
| §9 Errors / Redis lock | 07, 09 |
| §10 Testing | embedded per task |
| §11 Deployment | 09 |
| §13 Defaults | 02 seeds, 04 defaults, 07 timeouts |

## Task count (for subagent capacity planning)

| Plan | Tasks | Typical parallel slots |
|------|------:|------------------------|
| 01 | 6 | 1 |
| 02 | 5 | 1 (within plan serial) |
| 03 | 4 | 1 |
| 04 | 4 | 1 |
| 05 | 4 | 1 |
| 06 | 5 (06.0 serial + 4 parallel) | 4 after 06.0 |
| 07 | 6 | 1 |
| 08 | 8 | up to 4 after 08.3 |
| 09 | 4 | 1 |
| **Total** | **~46** | Wave2 peak **~7–8** |

## Self-review notes (2026-07-23)

- Placeholder scan: no TBD/TODO left in task bodies; Alpaca is explicit stub class in 05.3.
- Type consistency: agent ports 8001–8005 match `deploy/env.example` in Plan 01; Go DTO paths match `api_dto.md`.
- Gap closed: partial auto-exec + per-order approval covered in Plan 07.3–07.4; NAV upsert after fills in 03.3 + 07.
- Gap closed: `hold` intents must be ignored by portfolio agent (add assertion in Task 06.3 Step 1 — skip `hold`).
