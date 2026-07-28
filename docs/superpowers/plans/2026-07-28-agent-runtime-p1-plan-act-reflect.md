# P1 Plan / Act / Reflect Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat `call_model ↔ tools` loop with a domain plan → act → reflect → finalize runtime for Analyst and Portfolio, with explicit plan state, working memory, same-model JSON repair, and a minimal `trace.events[]` skeleton (UI/LangSmith stay P2; Go handoff injection stays P3).

**Architecture:** Add `plan_loop.py` (LangGraph) that owns plan/reflect/finalize nodes and reuses `execute_tool_call` / `openai_tool_schema` from `loop.py`. Extend mock tool scripts with `plan` + `reflect[]` sections; `ToolLLMClient` gains mock/live helpers for plan and reflect JSON turns. Envelope may include optional `handoff` / `working_memory`; trace may include `events`, final `plan`, and `working_memory` snapshots (schema updated). Analyst/Portfolio switch from `run_tool_loop` to `run_plan_loop`.

**Tech Stack:** Python 3, LangGraph, httpx/ToolLLMClient + ModelRouter (P0), pytest, JSON Schema contracts

**Spec:** `docs/superpowers/specs/2026-07-28-agent-runtime-plan-router-design.md` (§3 P1; handoff shape from §4 for finalize only; §5 events skeleton only)

## Global Constraints

- Plan loop scope: Analyst **and** Portfolio (same state machine; different tools/schemas/prompts)
- Keep existing `analyst_result` / `portfolio_result` as trading `result` contracts
- Parse/schema failure: **one same-model repair**, then baseline/hold — **never** switch provider (P0 ModelRouter)
- Fallback trigger remains HTTP/network only (do not change ModelRouter)
- Mock path must stay CI-green without live LLM (`LLM_MODE=mock` + extended scripts)
- P1 may write minimal `trace.events[]`; Runs UI + LangSmith are **P2**
- Go injecting Analyst `handoff`/`working_memory` into Portfolio request is **P3** (Python may emit them now)
- Agents never place orders; Go risk unchanged
- Do not commit secrets

## File map

| File | Responsibility |
|------|----------------|
| `services/agents/runtime/app/graphs/plan_types.py` | PlanStep, WorkingMemory, ReflectDecision helpers + normalization |
| `services/agents/runtime/app/graphs/plan_loop.py` | LangGraph plan → act ⇄ tools → reflect → finalize |
| `services/agents/runtime/app/graphs/loop.py` | Keep shared `execute_tool_call`, `openai_tool_schema`, `max_rounds_for`; `run_tool_loop` can remain for tests until callers migrate, then delete or thin-wrap |
| `services/agents/common/stock_agents_common/llm_tools.py` | Mock/live `complete_plan` / `complete_reflect` (or phase helpers) |
| `packages/contracts/fixtures/mock_tool_scripts/analyst.json` | Add `plan` + `reflect[]` |
| `packages/contracts/fixtures/mock_tool_scripts/portfolio.json` | Add `plan` + `reflect[]` |
| `packages/contracts/agent_run_response.schema.json` | Allow optional `handoff`, `working_memory`; allow trace `events`/`plan`/`working_memory`/`router` |
| `packages/contracts/agent_handoff.schema.json` | Analyst handoff shape (loose) |
| `packages/contracts/fixtures/agent_handoff.valid.json` | Fixture |
| `services/agents/runtime/app/graphs/analyst.py` | Prompts + `run_plan_loop` |
| `services/agents/runtime/app/graphs/portfolio.py` | Prompts + `run_plan_loop` + size_proposals baseline |
| `services/agents/runtime/tests/test_plan_types.py` | Unit tests for helpers |
| `services/agents/runtime/tests/test_plan_loop.py` | Graph unit tests with injected fake client |
| `services/agents/runtime/tests/test_analyst_graph.py` | Update for plan scripts |
| `services/agents/runtime/tests/test_portfolio_graph.py` | Update for plan scripts |
| `services/agents/common/tests/test_llm_tools_mock.py` | Plan/reflect mock helpers |

---

### Task 1: Plan state helpers + handoff contract

**Files:**
- Create: `services/agents/runtime/app/graphs/plan_types.py`
- Create: `services/agents/runtime/tests/test_plan_types.py`
- Create: `packages/contracts/agent_handoff.schema.json`
- Create: `packages/contracts/fixtures/agent_handoff.valid.json`
- Modify: `packages/contracts/agent_run_response.schema.json`

**Interfaces:**
- Produces:
  - `normalize_plan_steps(raw: Any) -> list[dict]` with keys `id`, `title`, `status` ∈ `pending|in_progress|done|skipped`, optional `tool_hint`
  - `empty_working_memory() -> dict` with `notes`, `evidence_refs`, `open_questions` (all `list[str]`)
  - `append_evidence_ref(memory: dict, ref: str) -> None`
  - `normalize_reflect(raw: Any) -> dict` with `decision` ∈ `continue|mark_step_done|revise_plan|finalize`, optional `step_id`, `reason`, `plan_patch`
  - `validate(data, "agent_handoff")` works via existing `stock_agents_common.schemas.validate`

- [ ] **Step 1: Write failing tests for helpers + handoff fixture validate**

```python
"""Unit tests for plan/reflect state helpers."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.graphs.plan_types import (
    append_evidence_ref,
    empty_working_memory,
    normalize_plan_steps,
    normalize_reflect,
)
from stock_agents_common.schemas import validate

ROOT = Path(__file__).resolve().parents[4]


def test_normalize_plan_steps_assigns_defaults():
    steps = normalize_plan_steps(
        [{"id": "s1", "title": "Fetch bars"}, {"title": "News only"}]
    )
    assert steps[0]["status"] == "pending"
    assert steps[0]["id"] == "s1"
    assert steps[1]["id"]  # auto id
    assert steps[1]["title"] == "News only"


def test_normalize_reflect_decisions():
    assert normalize_reflect({"decision": "finalize", "reason": "done"})["decision"] == "finalize"
    with pytest.raises(ValueError):
        normalize_reflect({"decision": "nope"})


def test_working_memory_evidence():
    mem = empty_working_memory()
    append_evidence_ref(mem, "get_daily_bars:AAPL")
    assert mem["evidence_refs"] == ["get_daily_bars:AAPL"]


def test_agent_handoff_fixture_validates():
    data = json.loads(
        (ROOT / "packages/contracts/fixtures/agent_handoff.valid.json").read_text(encoding="utf-8")
    )
    validate(data, "agent_handoff")
```

- [ ] **Step 2: Run tests — expect import fail**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_types.py -v
```

Expected: FAIL (`ModuleNotFoundError` / missing fixture)

- [ ] **Step 3: Implement helpers + schemas**

`agent_handoff.schema.json` (loose):

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "agent_handoff.schema.json",
  "type": "object",
  "properties": {
    "thesis_by_symbol": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "properties": {
          "summary": { "type": "string" },
          "bias": { "type": "string", "enum": ["bull", "bear", "neutral"] },
          "confidence": { "type": "number", "minimum": 0, "maximum": 1 }
        },
        "additionalProperties": true
      }
    },
    "open_questions": { "type": "array", "items": { "type": "string" } },
    "evidence_refs": { "type": "array", "items": { "type": "string" } },
    "confidence_notes": { "type": "string" }
  },
  "additionalProperties": true
}
```

Ensure `stock_agents_common.schemas` discovers new schema the same way as others (same directory convention).

Update `agent_run_response.schema.json`:
- Envelope `additionalProperties` stays false but add optional properties: `handoff`, `working_memory`
- Trace properties: add optional `events` (array of objects), `plan`, `working_memory`, `router`; keep `additionalProperties: false` on trace **or** set trace `additionalProperties: true` if listing every key is brittle — prefer explicit optional keys for the four above

- [ ] **Step 4: Tests PASS**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_types.py -v
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/runtime/app/graphs/plan_types.py services/agents/runtime/tests/test_plan_types.py packages/contracts/agent_handoff.schema.json packages/contracts/fixtures/agent_handoff.valid.json packages/contracts/agent_run_response.schema.json
git commit -m "feat(runtime): add plan state helpers and agent_handoff contract"
```

---

### Task 2: Extend mock scripts + ToolLLMClient plan/reflect APIs

**Files:**
- Modify: `services/agents/common/stock_agents_common/llm_tools.py`
- Modify: `services/agents/common/tests/test_llm_tools_mock.py`
- Modify: `packages/contracts/fixtures/mock_tool_scripts/analyst.json`
- Modify: `packages/contracts/fixtures/mock_tool_scripts/portfolio.json`

**Interfaces:**
- Produces on `ToolLLMClient`:
  - `complete_plan(system: str, user: str) -> dict` → `{content, plan_steps, usage, latency_ms, router?}`  
    Mock: read `script["plan"]` once (do not consume `rounds`).  
    Live: chat completions **without tools**, parse JSON `{steps:[...]}` from content.
  - `complete_reflect(system: str, messages: list) -> dict` → `{content, reflect, usage, latency_ms, router?}`  
    Mock: consume next entry from `script["reflect"]` list (separate index `_reflect_index`).  
    Live: no tools; parse JSON reflect object.
  - `reset()` also resets `_reflect_index` and plan-consumed flag
- `complete_tools` unchanged for act rounds (`script["rounds"]`)

- [ ] **Step 1: Write failing tests**

```python
def test_mock_complete_plan_and_reflect_do_not_consume_tool_rounds(monkeypatch, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = {
        "plan": {"steps": [{"id": "s1", "title": "Bars", "status": "pending"}]},
        "reflect": [
            {"decision": "mark_step_done", "step_id": "s1", "reason": "ok"},
            {"decision": "finalize", "reason": "done"},
        ],
        "rounds": [{"tool_calls": [{"id": "1", "name": "get_news", "args": {"symbol": "AAPL"}}]}],
    }
    path = tmp_path / "script.json"
    path.write_text(json.dumps(script), encoding="utf-8")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(path))

    client = ToolLLMClient()
    plan = client.complete_plan("sys", "user")
    assert plan["plan_steps"][0]["id"] == "s1"
    tools = client.complete_tools("sys", [{"role": "user", "content": "x"}], [])
    assert tools["tool_calls"][0]["name"] == "get_news"
    r1 = client.complete_reflect("sys", [])
    assert r1["reflect"]["decision"] == "mark_step_done"
    r2 = client.complete_reflect("sys", [])
    assert r2["reflect"]["decision"] == "finalize"
```

- [ ] **Step 2: Run — expect AttributeError**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_tools_mock.py::test_mock_complete_plan_and_reflect_do_not_consume_tool_rounds -v
```

- [ ] **Step 3: Implement APIs + update default fixtures**

Analyst fixture shape (keep existing rounds; prepend plan/reflect):

```json
{
  "plan": {
    "steps": [
      {"id": "s1", "title": "Account view", "status": "pending", "tool_hint": "get_account_view"},
      {"id": "s2", "title": "Daily bars", "status": "pending", "tool_hint": "get_daily_bars"}
    ]
  },
  "reflect": [
    {"decision": "mark_step_done", "step_id": "s1", "reason": "account loaded"},
    {"decision": "mark_step_done", "step_id": "s2", "reason": "bars loaded"},
    {"decision": "finalize", "reason": "enough evidence"}
  ],
  "rounds": [ /* existing three rounds */ ]
}
```

**Ordering note for the graph:** after each tools batch, one reflect call; after plan, act uses rounds. Fixture `reflect` length must match how many times the graph will call reflect for the happy path (implementer must align fixture with Task 3 control flow — document in Task 3).

Live `complete_plan` / `complete_reflect`: reuse `chat_completions` via same path as `_complete_real` with `tools=[]` and JSON extraction via `extract_json_from_content`.

- [ ] **Step 4: Existing + new llm_tools tests PASS**

```powershell
cd services/agents/common
python -m pytest tests/test_llm_tools_mock.py tests/test_model_router.py -v
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/common/stock_agents_common/llm_tools.py services/agents/common/tests/test_llm_tools_mock.py packages/contracts/fixtures/mock_tool_scripts/analyst.json packages/contracts/fixtures/mock_tool_scripts/portfolio.json
git commit -m "feat(agents): mock plan/reflect APIs and extend tool scripts"
```

---

### Task 3: Implement `run_plan_loop` LangGraph

**Files:**
- Create: `services/agents/runtime/app/graphs/plan_loop.py`
- Create: `services/agents/runtime/tests/test_plan_loop.py`
- Modify: `services/agents/runtime/app/graphs/loop.py` (export helpers only; do not delete `run_tool_loop` yet)

**Interfaces:**
- Produces: `run_plan_loop(*, agent, req, system_plan, system_act, system_reflect, user_message, tools_schema, tool_registry, result_schema, align_result=None, baseline=None, llm_client=None, ctx=None, ensure_size_proposals=False, build_handoff=None) -> dict`  
  Return envelope `{result, trace, handoff?, working_memory?}` validating `agent_run_response`.

**Graph control flow (lock this):**

```text
entry: plan_node
plan_node → act_model
act_model → tools | reflect   # tools if tool_calls; else treat as step note → reflect
tools → reflect
reflect → act_model | plan_node | finalize_node
  continue / mark_step_done (more pending) → act_model
  revise_plan → plan_node (re-plan; clear or merge patch)
  finalize → finalize_node
finalize_node → END
```

**State (`PlanLoopState` TypedDict):**  
`messages`, `plan`, `current_step_id`, `working_memory`, `round_i`, `stop_reason`, `result`, `handoff`, `last_tool_calls`, `last_content`, `reflect_decision`

**Round budget:** reuse `max_rounds_for`; each `act_model` invocation increments `round_i`; on exhaustion, finalize with baseline/hold like today.

**Trace:** keep appending `rounds` for act/tool as today; also append minimal `events` list:
`{type, at, ...}` for `plan`, `step_start`, `llm`, `tool`, `reflect`, `finalize` (P2 will render; P1 only writes).

- [ ] **Step 1: Write failing graph test with FakeClient**

```python
class FakeClient:
    def __init__(self):
        self.n = 0
    def complete_plan(self, system, user):
        return {"plan_steps": [{"id": "s1", "title": "News", "status": "pending"}], "usage": {}, "latency_ms": 1}
    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {"content": None, "tool_calls": [{"id": "1", "name": "get_news", "args": {"symbol": "AAPL"}}], "usage": {}, "latency_ms": 1}
        return {
            "content": json.dumps({
                "items": [{
                    "symbol": "AAPL", "bias": "bull", "confidence": 0.7,
                    "thesis": "t", "side": "buy", "urgency": "normal", "rationale": "r"
                }],
                "warnings": []
            }),
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }
    def complete_reflect(self, system, messages):
        if self.n == 1:
            return {"reflect": {"decision": "mark_step_done", "step_id": "s1", "reason": "got news"}, "usage": {}, "latency_ms": 1}
        return {"reflect": {"decision": "finalize", "reason": "done"}, "usage": {}, "latency_ms": 1}
```

Register a tiny `get_news` tool returning `{"ok": True, "data": {"headlines": []}}`. Call `run_plan_loop` for analyst-like schema with watchlist `["AAPL"]`. Assert `result` validates, `trace["events"]` non-empty, `stop_reason == "final"`.

- [ ] **Step 2: Run — expect import fail**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_loop.py -v
```

- [ ] **Step 3: Implement `plan_loop.py`**

Reuse `execute_tool_call`, `append_round`, `new_trace`, `finalize_trace`, `extract_json_from_content`, `validate`, ModelRouter-aware `resp.get("router")` into llm event/round meta (same as current loop).

On tools success, `append_evidence_ref(working_memory, f"{name}:{ok}")` (keep refs short).

`finalize_node`:
1. Try parse last content / candidate JSON → `align_result` → `validate(result_schema)`
2. On failure: one repair call — `complete_tools` or a dedicated repair prompt via `complete_reflect`/`complete_tools` with system “fix JSON to schema …” (**same client / provider**)
3. Still fail → baseline/hold path (copy from `run_tool_loop` fallback behavior)
4. Optional `build_handoff(result, working_memory, req)`; if present, `validate(handoff, "agent_handoff")` when non-empty
5. Attach `working_memory`, `plan`, `events` onto `trace`; envelope includes `handoff`/`working_memory` when set

- [ ] **Step 4: Tests PASS**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_loop.py -v
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/runtime/app/graphs/plan_loop.py services/agents/runtime/tests/test_plan_loop.py
git commit -m "feat(runtime): add plan/act/reflect LangGraph loop"
```

---

### Task 4: Wire Analyst + Portfolio + same-model repair path

**Files:**
- Modify: `services/agents/runtime/app/graphs/analyst.py`
- Modify: `services/agents/runtime/app/graphs/portfolio.py`
- Modify: `services/agents/runtime/tests/test_analyst_graph.py`
- Modify: `services/agents/runtime/tests/test_portfolio_graph.py`
- Modify: `services/agents/runtime/tests/test_http.py` (if needed)
- Modify: mock fixtures if reflect/round counts mismatch after real graph runs

**Interfaces:**
- `run_analyst` / `run_portfolio` call `run_plan_loop` instead of `run_tool_loop`
- Provide three system prompts (or one act prompt + dedicated plan/reflect strings)
- Analyst `build_handoff`: map `result.items` → `thesis_by_symbol` + `evidence_refs` from working_memory
- Portfolio: keep `ensure_size_proposals=True` and baseline behavior; `build_handoff` may return `{}` / omit

- [ ] **Step 1: Update prompts (sketch)**

Plan system: “Output JSON `{steps:[{id,title,status,tool_hint?}]}` covering evidence gathering for the watchlist. Do not call tools yet.”

Act system: existing tool instructions + “Work only on current_step; call tools or say step complete.”

Reflect system: “Given plan + last tools, return JSON `{decision, step_id?, reason, plan_patch?}` where decision is continue|mark_step_done|revise_plan|finalize.”

- [ ] **Step 2: Switch callers to `run_plan_loop`; run existing graph tests**

```powershell
cd services/agents/runtime
python -m pytest tests/test_analyst_graph.py tests/test_portfolio_graph.py tests/test_http.py tests/test_plan_loop.py -v
```

Expected: failures until fixtures/reflect counts align — fix fixtures/tests to assert `trace["plan"]` or events contain `type=="plan"` where appropriate; keep validating `analyst_result` / `portfolio_result` / `agent_run_response`.

- [ ] **Step 3: Add one repair unit test**

Fake client returns invalid JSON content once on finalize path, then valid JSON on repair call; assert final envelope validates and only one repair attempt (counter).

- [ ] **Step 4: Full runtime suite PASS**

```powershell
cd services/agents/runtime
python -m pytest tests/ -v
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/runtime/app/graphs/analyst.py services/agents/runtime/app/graphs/portfolio.py services/agents/runtime/tests packages/contracts/fixtures/mock_tool_scripts
git commit -m "feat(runtime): wire analyst/portfolio onto plan/act/reflect loop"
```

---

### Task 5: Retire flat loop entry + docs touch

**Files:**
- Modify: `services/agents/runtime/app/graphs/loop.py` — remove `run_tool_loop` **or** make it a thin deprecated wrapper calling `run_plan_loop` with a single synthetic plan step (prefer **delete** if no callers remain)
- Modify: `docs/product-overview.md` — §3.2 note plan/act/reflect (one short paragraph)
- Modify: `README.md` only if mock script docs mention rounds-only format

**Interfaces:**
- No remaining imports of `run_tool_loop` outside `loop.py`

- [ ] **Step 1: Grep for callers**

```powershell
rg "run_tool_loop" services/agents
```

Expected: only plan_loop migration leftovers — delete or update.

- [ ] **Step 2: Delete/wrapper + update product-overview**

- [ ] **Step 3: Run common + runtime tests**

```powershell
cd services/agents/common; python -m pytest tests/test_llm_tools_mock.py tests/test_model_router.py -q
cd ../runtime; python -m pytest tests/ -q
```

Expected: all PASS

- [ ] **Step 4: Commit**

```powershell
git add services/agents/runtime/app/graphs/loop.py docs/product-overview.md README.md
git commit -m "refactor(runtime): remove flat tool loop; document plan/act/reflect"
```

---

## P1 Self-review checklist

1. **Spec §3:** plan/act/reflect/finalize for both agents; state fields; mock scripts; same-model repair — Tasks 1–4.
2. **Spec §4 (partial):** handoff schema + emission on Analyst envelope — Task 1 + 4; Go injection **not** in this plan.
3. **Spec §5 (partial):** `events[]` skeleton written — Task 3; UI/LangSmith **not** in this plan.
4. **No provider switch on parse failure** — repair uses same client; ModelRouter untouched.
5. **Types:** `complete_plan` / `complete_reflect` / `run_plan_loop` names consistent across tasks.

## Out of scope (next plans)

- P2: Runs timeline UI + LangSmith
- P3: Go passes Analyst `handoff` + `working_memory` into Portfolio request

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-28-agent-runtime-p1-plan-act-reflect.md`.**

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks  
2. **Inline Execution** — execute in this session with executing-plans checkpoints  

Which approach?
