# P2 Observability: Agent Timeline UI + LangSmith Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Runs detail show a real agent timeline from `trace.events[]` (plus handoff summary), keep the existing tool-round expander, and optionally export traces to LangSmith when env-enabled — without breaking mock/CI or the trading path.

**Architecture:** P1 already writes `trace.events`, `trace.plan`, `trace.working_memory`. P2 hardens Python side (router snapshot, handoff event, optional `langsmith_url`), extends web types + Runs UI components, and wires LangSmith via env (`LANGSMITH_TRACING` / `LANGSMITH_API_KEY`) around `run_plan_loop` / graph invoke with fail-open behavior.

**Tech Stack:** Next.js (App Router) + Vitest + Testing Library; Python agent-runtime (`langsmith` via existing langchain-core stack); Docker Compose env passthrough

**Spec:** `docs/superpowers/specs/2026-07-28-agent-runtime-plan-router-design.md` (§5)

## Global Constraints

- Local Runs `payload_json.trace` remains the **authoritative** full audit trail
- LangSmith is optional parallel export; default **off**; export failures must not fail trading
- Keep `rounds[]` tool expander for backward compatibility; old runs without `events` fall back to rounds-only UI
- Event types: `plan`, `step_start`, `llm`, `tool`, `reflect`, `handoff`, `finalize`
- Do not commit secrets; document placeholder env keys only
- No Go handoff injection (P3); UI may still display handoff when present on envelope
- Preview-truncate large payloads before LangSmith (reuse `result_preview` / length limits)

## File map

| File | Responsibility |
|------|----------------|
| `services/agents/runtime/app/graphs/plan_loop.py` | Attach `trace.router` summary; emit `handoff` event; optional langsmith URL field |
| `services/agents/common/stock_agents_common/observability.py` (new) | LangSmith enable helper + safe run wrapper / metadata |
| `services/agents/common/tests/test_observability.py` | Unit tests: disabled by default; fail-open |
| `deploy/docker-compose.yml` | Pass `LANGSMITH_*` |
| `deploy/env.example` / `README.md` | Document LangSmith env |
| `apps/web/src/lib/types.ts` | `AgentTraceEvent`, extend `AgentTrace` / `AgentRunEnvelope` |
| `apps/web/src/components/AgentTimeline.tsx` (new) | Render events timeline |
| `apps/web/src/components/HandoffSummary.tsx` (new) | Compact handoff display |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | Wire timeline + handoff + optional LangSmith link |
| `apps/web/src/app/globals.css` | Timeline styles (extend `.runs__*`) |
| `apps/web/src/app/(shell)/runs/[id]/page.test.tsx` | UI tests for timeline / fallback |
| `packages/contracts/fixtures/agent_run_response.valid.json` | Optional events in fixture for web tests |

---

### Task 1: Trace polish — router snapshot + handoff event

**Files:**
- Modify: `services/agents/runtime/app/graphs/plan_loop.py`
- Modify: `services/agents/runtime/tests/test_plan_loop.py` and/or `test_analyst_graph.py`

**Interfaces:**
- On successful finalize, set `trace["router"]` to a small dict, e.g. `{ "fallback_used_any": bool, "models": string[] }` aggregated from llm events / rounds (YAGNI: at least `fallback_used_any` from any `llm` event with `fallback_used`)
- When envelope includes non-empty `handoff`, append event `{type:"handoff", at, handoff_preview}` where preview is truncated JSON (`result_preview(handoff)`)
- Do not require LangSmith yet

- [ ] **Step 1: Write failing assertions**

In an existing mock analyst/plan_loop test that returns handoff, assert:

```python
assert any(e.get("type") == "handoff" for e in out["trace"].get("events") or [])
assert "router" in out["trace"]
```

If current mock path has empty handoff, assert at least `trace["router"]` is a dict after finalize.

- [ ] **Step 2: Run — expect fail**

```powershell
cd services/agents/runtime
python -m pytest tests/test_analyst_graph.py tests/test_plan_loop.py -q
```

- [ ] **Step 3: Implement in `plan_loop.py` finalize path**

- [ ] **Step 4: Tests PASS + commit**

```powershell
git add services/agents/runtime/app/graphs/plan_loop.py services/agents/runtime/tests
git commit -m "feat(runtime): add router snapshot and handoff trace events"
```

---

### Task 2: LangSmith wiring (fail-open, default off)

**Files:**
- Create: `services/agents/common/stock_agents_common/observability.py`
- Create: `services/agents/common/tests/test_observability.py`
- Modify: `services/agents/runtime/app/graphs/plan_loop.py` (or `runtime/app/main.py` entry) to call under tracing context when enabled
- Modify: `deploy/docker-compose.yml`, `deploy/env.example`, `README.md`

**Interfaces:**
- `tracing_enabled() -> bool` — true iff `LANGSMITH_TRACING` in `{true,1,yes}` **and** `LANGSMITH_API_KEY` non-empty
- `run_with_tracing(name: str, fn: Callable[[], T], metadata: dict | None = None) -> T` — if disabled, call `fn()`; if enabled, wrap with `langsmith.run_helpers.tracing_context` / `traceable` equivalent; on any exception from tracing setup, log warning and still call `fn()`
- Optional: set `trace["langsmith_run_url"]` only when a URL is actually available (if SDK does not easily expose URL, skip URL and document “view in LangSmith project” — do not invent URLs)

**Env (compose passthrough):**

```yaml
LANGSMITH_TRACING: ${LANGSMITH_TRACING:-false}
LANGSMITH_API_KEY: ${LANGSMITH_API_KEY:-}
LANGSMITH_PROJECT: ${LANGSMITH_PROJECT:-}
LANGSMITH_ENDPOINT: ${LANGSMITH_ENDPOINT:-}
```

- [ ] **Step 1: Unit tests**

```python
def test_tracing_disabled_by_default(monkeypatch):
    monkeypatch.delenv("LANGSMITH_TRACING", raising=False)
    monkeypatch.delenv("LANGSMITH_API_KEY", raising=False)
    from stock_agents_common.observability import tracing_enabled
    assert tracing_enabled() is False

def test_run_with_tracing_invokes_fn_when_disabled(monkeypatch):
    monkeypatch.setenv("LANGSMITH_TRACING", "false")
    from stock_agents_common.observability import run_with_tracing
    assert run_with_tracing("t", lambda: 42) == 42
```

- [ ] **Step 2: Implement + wrap `run_plan_loop` body** (innermost work stays identical)

Prefer wrapping at the start of `run_plan_loop` so plan/act/reflect LLM calls are nested under one root run when LangChain auto-instrumentation applies; if LangGraph does not auto-export without callbacks, use `langsmith.traceable` on `run_plan_loop` gated by env — keep YAGNI: env gate + `traceable` decorator pattern is enough for P2.

- [ ] **Step 3: Docs + compose**

- [ ] **Step 4: Tests PASS + commit**

```powershell
cd services/agents/common
python -m pytest tests/test_observability.py -v
cd ../runtime
python -m pytest tests/ -q
git add services/agents/common services/agents/runtime/app/graphs/plan_loop.py deploy/docker-compose.yml deploy/env.example README.md
git commit -m "feat(agents): optional LangSmith tracing (default off)"
```

---

### Task 3: Web types + AgentTimeline + HandoffSummary

**Files:**
- Modify: `apps/web/src/lib/types.ts`
- Create: `apps/web/src/components/AgentTimeline.tsx`
- Create: `apps/web/src/components/HandoffSummary.tsx`
- Create: `apps/web/src/components/AgentTimeline.test.tsx` (or page-level tests only — prefer component unit test)
- Modify: `apps/web/src/app/globals.css`

**Interfaces:**
- Types:

```ts
export type AgentTraceEventType =
  | "plan"
  | "step_start"
  | "llm"
  | "tool"
  | "reflect"
  | "handoff"
  | "finalize";

export interface AgentTraceEvent {
  type: AgentTraceEventType | string;
  at?: string;
  [key: string]: unknown;
}

export interface AgentTrace {
  // existing fields...
  events?: AgentTraceEvent[];
  plan?: unknown;
  working_memory?: unknown;
  router?: { fallback_used_any?: boolean; [key: string]: unknown };
  langsmith_run_url?: string;
}

export interface AgentRunEnvelope {
  result: ...;
  trace: AgentTrace;
  handoff?: Record<string, unknown>;
  working_memory?: Record<string, unknown>;
}
```

- `AgentTimeline({ events }: { events: AgentTraceEvent[] })` — ordered list; each item shows type badge, time, and a one-line summary (e.g. tool name, reflect decision, model + fallback_used)
- If `events` empty/missing → render null or a short “No agent timeline” (parent decides fallback to rounds)
- `HandoffSummary({ handoff })` — show thesis_by_symbol count / open_questions / confidence_notes when present; otherwise null

- [ ] **Step 1: Failing component test** — render sample events, assert “plan” / “reflect” text visible

- [ ] **Step 2: Implement components + CSS** (reuse `.runs__timeline` patterns; avoid purple-glow AI defaults; match existing desk UI)

- [ ] **Step 3: Tests PASS + commit**

```powershell
cd apps/web
npm test -- --run src/components/AgentTimeline.test.tsx
git add apps/web/src
git commit -m "feat(web): add AgentTimeline and HandoffSummary components"
```

---

### Task 4: Wire Runs detail page

**Files:**
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.test.tsx`
- Optionally update fixture `packages/contracts/fixtures/agent_run_response.valid.json` with sample `events` (keep schema-valid)

**Behavior:**
- In `StepPayload` / envelope branch: if `trace.events?.length`, show **Agent timeline** (button toggle or always-on section above tool trace)
- Show `HandoffSummary` when `envelope.handoff` present
- Keep **Show tool trace** expander unchanged
- If `trace.langsmith_run_url`, show external link “Open in LangSmith”
- Old envelopes without events: no timeline section; tool rounds still work

- [ ] **Step 1: Extend page test** with envelope including events + handoff; click/show timeline; assert labels

- [ ] **Step 2: Implement page wiring**

- [ ] **Step 3: `npm test -- --run` for runs page + components PASS**

- [ ] **Step 4: Commit**

```powershell
git add apps/web packages/contracts/fixtures/agent_run_response.valid.json
git commit -m "feat(web): show agent timeline and handoff on Runs detail"
```

---

### Task 5: Smoke docs + product overview note

**Files:**
- Modify: `docs/product-overview.md` §3.5 / observability — one short note on timeline + optional LangSmith
- Modify: `deploy/env.example` if Task 2 missed any keys

- [ ] **Step 1: Edit docs**

- [ ] **Step 2: Commit**

```powershell
git add docs/product-overview.md deploy/env.example
git commit -m "docs: document Runs timeline and LangSmith env"
```

---

## P2 Self-review checklist

1. Spec §5 local events + UI + LangSmith optional — Tasks 1–5
2. Fail-open tracing; default off
3. Old runs without events still usable
4. No P3 Go injection scope creep
5. Secrets not committed

## Out of scope

- P3: Go injects Analyst handoff/working_memory into Portfolio
- Full LangSmith SaaS dashboard clone / self-host

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-28-agent-runtime-p2-observability.md`.**

**Two execution options:**

1. **Subagent-Driven (recommended)**
2. **Inline Execution**

Which approach?
