# Agent Runtime: Model Router + Plan/Act/Reflect + Observability — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation  
**Approach:** Domain agent runtime on LangGraph (custom plan/act/reflect orchestration; not a from-scratch graph engine)

## Goal

Upgrade `agent-runtime` so that:

1. **Model router** — primary Volcengine Ark `Doubao-Smart-Router`, fallback to MiniMax on HTTP/network failure only.
2. **Plan → Act → Reflect** — explicit plan, step state, autonomous tool choice, reflection (Analyst **and** Portfolio).
3. **Hybrid agent handoff** — keep machine `result` schemas; add validated `handoff` for the next agent.
4. **Run-scoped memory** — `working_memory` within one workflow run, passed Analyst → Portfolio via Go.
5. **Observability** — local Runs `trace` is the authoritative full timeline; LangSmith is optional parallel export.

## Locked decisions

| Topic | Choice |
|-------|--------|
| Architecture | Extend existing LangGraph tool loop (Approach 1), not CrewAI/AutoGen rewrite |
| Runtime framing | Self-developed **domain agent loop runtime** on LangGraph |
| Primary LLM | Volcengine Ark OpenAI-compatible API; model `Doubao-Smart-Router` (or user `ep-...` endpoint id via env) |
| Fallback LLM | Existing MiniMax (`LLM_FALLBACK_*` / migrated `LLM_*`) |
| Fallback trigger | HTTP/network failures only (timeouts, connection errors, status ≥ 400) |
| Parse/schema failure | Same-model repair once, then baseline/hold — **do not** switch provider |
| Delivery | P0 → P1 → P2 → P3 as sequential PRs |
| Plan loop scope | Analyst **and** Portfolio |
| Handoff | `result` (existing schemas) + structured `handoff` (JSON Schema) |
| Memory | Within one workflow run, across agents; **no** cross-run memory |
| Observability | Expand local `trace` (authoritative) **and** LangSmith when enabled |
| Trading authority | Unchanged: Go risk + Alpaca; agents never place orders |

## Non-goals

- Cross-run / long-term memory or vector stores
- App-side task-type model picking (delegated to Doubao-Smart-Router)
- Removing `analyst_result` / `portfolio_result` JSON Schema contracts
- Replacing local Runs audit with SaaS-only traces
- Agents bypassing Go risk or calling Alpaca Trading

---

## §1 Architecture

```text
Go WorkflowRunner
  → agent-runtime Analyst (plan/act/reflect + tools)
       envelope: { result, handoff, trace, working_memory? }
  → agent-runtime Portfolio (prior envelope injected; plan/act/reflect)
       envelope: { result, handoff?, trace }
  → Go risk / orders (reads portfolio.result.proposals only)

LLM: ModelRouter(primary=Volcengine Doubao-Smart-Router, fallback=MiniMax)
Obs: local trace.events authoritative in Postgres; LANGSMITH_TRACING=true exports in parallel
```

### Positioning

| Layer | Ownership |
|-------|-----------|
| Graph engine | LangGraph (`StateGraph`, nodes, edges, `invoke`) |
| Domain runtime | This project: plan/act/reflect state machine, ModelRouter, handoff/memory contracts, local trace |

---

## §2 P0 — Model Router

### Env

| Variable | Role |
|----------|------|
| `LLM_PRIMARY_API_KEY` / `LLM_PRIMARY_BASE_URL` / `LLM_PRIMARY_MODEL` | Volcengine primary (default base `https://ark.cn-beijing.volces.com/api/v3`, default model `Doubao-Smart-Router`) |
| `LLM_FALLBACK_API_KEY` / `LLM_FALLBACK_BASE_URL` / `LLM_FALLBACK_MODEL` | MiniMax fallback |
| Compatibility | If `LLM_PRIMARY_*` unset, use existing `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` as single provider (no failover) |

### Behavior

1. `ToolLLMClient` / `LLMClient` call through shared `ModelRouter.complete(...)`.
2. On primary HTTP/network failure, retry **once** with fallback in the same call.
3. Both fail → raise; record errors on trace/router metadata.
4. Record per call: `provider`, `model`, `fallback_used`, `error?`.

### Out of scope for P0

Circuit breakers, task-based routing, plan/reflect changes.

---

## §3 P1 — Plan / Act / Reflect + State

### Graph

```text
plan → act_model ⇄ tools → reflect → (continue act | revise plan | finalize)
```

| Node | Responsibility |
|------|----------------|
| `plan` | Emit `plan[]` (`id`, `title`, `status`, optional `tool_hint`); write State + trace event |
| `act_model` | For `current_step`, choose tool_calls or step completion; via ModelRouter |
| `tools` | Execute tools; append evidence to `working_memory.evidence_refs` |
| `reflect` | Decide `continue` / `mark_step_done` / `revise_plan` / `finalize` |
| `finalize` | Emit `result` + `handoff`; validate; stop |

### State

- `plan: [{ id, title, status: pending\|in_progress\|done\|skipped, tool_hint? }]`
- `current_step_id`
- `working_memory: { notes[], evidence_refs[], open_questions[] }`
- `messages`, `result`, `handoff`
- `router: { primary, active, fallback_used, last_error? }`
- `stop_reason`, round / max_rounds limits

### Tools

Existing registries unchanged by agent (Analyst vs Portfolio). Plan/reflect choose which tools to call; no hard-coded tool order. Portfolio keeps `size_proposals` baseline guarantee before finalize when needed.

### Finalization / parsing

- Keep existing `result` schemas as trading contracts.
- Prefer structured submit tool and/or stronger `extract_json_from_content`.
- On validate failure: one same-model repair attempt, then baseline/hold.

### Mock

Extend mock tool scripts with plan / reflect / act rounds so CI stays offline.

---

## §4 Handoff + run-scoped memory

### Envelope

```json
{
  "result": {},
  "handoff": {},
  "trace": {},
  "working_memory": {}
}
```

- Go persists full envelope in `payload_json`.
- Go injects Analyst `result` + `handoff` + `working_memory` into Portfolio request.
- Risk/orders use **only** `portfolio.result.proposals`.

### Analyst `handoff` schema (conceptual)

```json
{
  "thesis_by_symbol": {
    "AAPL": {
      "summary": "string",
      "bias": "bull|bear|neutral",
      "confidence": 0.0
    }
  },
  "open_questions": ["string"],
  "evidence_refs": ["string"],
  "confidence_notes": "string"
}
```

Validated but looser than `analyst_result` (missing symbols / empty lists allowed). Does **not** replace `result.items`.

Portfolio `handoff` optional (audit/UI); may be empty in P1/P3.

### Memory scope

| Scope | Behavior |
|-------|----------|
| Inside one agent invoke | Update `working_memory` across plan/act/reflect |
| Analyst → Portfolio | Go injects prior `working_memory` + `handoff` |
| Across runs | Not in this design |

---

## §5 Observability

### Local authoritative `trace`

Keep `rounds[]` for backward compatibility. Add `events[]` as the full timeline:

Event types: `plan`, `step_start`, `llm`, `tool`, `reflect`, `handoff`, `finalize`.

Top-level snapshots: final `plan`, `working_memory`, `router` summary.

### Runs UI

- Agent timeline from `events[]` (plan → step → llm/think → tools → reflect → handoff).
- Show `handoff` summary.
- Keep existing tool-round expander.
- Optional LangSmith run URL when present.

### LangSmith

- Env: `LANGSMITH_TRACING=true`, `LANGSMITH_API_KEY`, optional `LANGSMITH_PROJECT` / `LANGSMITH_ENDPOINT`.
- Default **off** (mock/CI).
- Export failures must not break the trading path; log local warning.
- Use preview truncation for large tool payloads.

---

## §6 Delivery, risks, success

### PR sequence

| PR | Scope | Verify |
|----|-------|--------|
| P0 | ModelRouter + env + router fields on trace | Unit tests with mock transport; optional live smoke |
| P1 | Plan/act/reflect for Analyst + Portfolio; mock scripts; parse repair | Graph unit + mock e2e |
| P2 | `trace.events[]` + Runs timeline UI + LangSmith wiring | UI tests; tracing unit with mock client |
| P3 | `handoff` schema + Go injection + run memory pass-through | Go workflow tests + contract fixtures |

P1 may write a minimal `events[]` skeleton; UI and LangSmith remain P2.

### Risks

| Risk | Mitigation |
|------|------------|
| Ark model id may be `ep-...` | Env-configurable; docs note endpoint ids |
| More LLM rounds → latency/cost | `max_rounds` + early finalize; CI uses mock |
| Sensitive data to LangSmith | Truncation; tracing off by default |
| Larger envelopes | Bounded handoff fields; preview limits |
| Old runs without `events` | UI falls back to `rounds` only |

### Success criteria

1. Live primary Volcengine; on primary HTTP failure, MiniMax used and visible in local trace.
2. Mock path completes plan→act→reflect→finalize with real `events` replayable in Runs.
3. Portfolio receives Analyst `handoff` + `working_memory`.
4. Risk/orders still depend only on `portfolio.result`.
5. With LangSmith enabled, runs appear there; with it disabled, local Runs remain complete.

## Implementation notes

- Touch primarily: `services/agents/common/stock_agents_common/{llm,llm_tools}.py`, new `model_router.py`, `runtime/app/graphs/{loop,analyst,portfolio}.py`, contracts schemas/fixtures, Go workflow request building, `apps/web` Runs detail, `deploy/docker-compose.yml` + `.env.example`.
- Do not commit secrets; document placeholder env keys only.
- Update `docs/product-overview.md` after P3 lands (or incrementally per PR).
