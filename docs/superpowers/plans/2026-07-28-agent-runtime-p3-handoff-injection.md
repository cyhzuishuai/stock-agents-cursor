# P3 Handoff Injection + Run Memory Pass-through Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go injects Analyst `handoff` + `working_memory` into Portfolio `prior_step_outputs` as sibling keys (`analyst_handoff`, `analyst_working_memory`); Portfolio plan/act user context consumes them; missing handoff does not fail the run.

**Architecture:** Keep `prior_step_outputs.analyst` as the Analyst **result** object (`items`). After decoding the Analyst envelope, copy optional `handoff` / `working_memory` into sibling map keys. Portfolio `_user_message` appends compact JSON of those keys when present. Risk/orders unchanged (still `portfolio.result.proposals` only).

**Tech Stack:** Go workflow runner + `httptest` stubs; Python Portfolio graph + pytest

**Spec:** `docs/superpowers/specs/2026-07-28-agent-runtime-p3-handoff-injection-design.md`

## Global Constraints

- Parallel keys only — do **not** nest under `prior_step_outputs.analyst`
- `size_proposals` continues to read `prior_step_outputs.analyst.items`
- Missing `handoff` / `working_memory` → omit sibling keys (or empty); never fail the workflow for absence
- Reuse `packages/contracts/agent_handoff.schema.json` (no new schema required)
- Do not commit secrets

## File map

| File | Responsibility |
|------|----------------|
| `services/api/internal/workflow/runner.go` | Decode handoff/working_memory; inject sibling keys into `prior` |
| `services/api/internal/workflow/runner_test.go` | Assert Portfolio request sees sibling keys; missing-handoff happy path |
| `services/agents/runtime/app/graphs/portfolio.py` | Include handoff/memory in `_user_message` |
| `services/agents/runtime/tests/test_portfolio_graph.py` | Assert user message / run still works with injected keys |
| `docs/product-overview.md` | One-line note: Analyst→Portfolio run memory via Go |

---

### Task 1: Go — envelope decode + sibling-key injection

**Files:**
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/workflow/runner_test.go`

**Interfaces:**
- Extend `agentEnvelope`:
  ```go
  type agentEnvelope struct {
  	Result         json.RawMessage `json:"result"`
  	Trace          json.RawMessage `json:"trace"`
  	Handoff        json.RawMessage `json:"handoff"`
  	WorkingMemory  json.RawMessage `json:"working_memory"`
  }
  ```
- After `prior[step.Name] = resultObj`, if `step.Name == StepAnalyst`:
  - If `len(envelope.Handoff) > 0` and not JSON `null`, unmarshal to `any` and set `prior["analyst_handoff"] = …`
  - If `len(envelope.WorkingMemory) > 0` and not JSON `null`, set `prior["analyst_working_memory"] = …`
- Portfolio step still receives the full `prior` map as today

- [ ] **Step 1: Write the failing Go tests**

Extend the runtime stub (or add a recording variant) so Portfolio calls can inspect `prior_step_outputs`. Minimal pattern — store last portfolio prior on the stub:

```go
type stubResponses struct {
	analyst, portfolio string
	failAt             string
	lastPortfolioPrior map[string]any // filled when agent=portfolio
}

// In startAgentRuntimeStub, after decoding req, if req.Agent == portfolio:
//   stubs.lastPortfolioPrior = req.PriorStepOutputs
```

Add helper for Analyst envelope with handoff + memory:

```go
func analystEnvelopeWithHandoffJSON() string {
	return `{
	  "result":{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]},
	  "handoff":{"thesis_by_symbol":{"AAPL":{"summary":"ok","bias":"bull","confidence":0.7}},"evidence_refs":["get_bars:AAPL"],"open_questions":["volume?"]},
	  "working_memory":{"notes":["n1"],"evidence_refs":["get_bars:AAPL"],"open_questions":["volume?"]},
	  "trace":{"agent":"analyst","rounds":[],"stop_reason":"final"}
	}`
}
```

Tests:

```go
func TestRunWorkflowInjectsAnalystHandoffIntoPortfolioPrior(t *testing.T) {
	stubs := stubResponses{
		analyst:   analystEnvelopeWithHandoffJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs) // ensure stubResponses is passed by pointer or shared so lastPortfolioPrior is visible
	_, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	prior := stubs.lastPortfolioPrior
	if prior == nil {
		t.Fatal("expected portfolio prior captured")
	}
	analyst, ok := prior["analyst"].(map[string]any)
	if !ok || analyst["items"] == nil {
		t.Fatalf("analyst must remain result-shaped with items: %#v", prior["analyst"])
	}
	if _, ok := prior["analyst_handoff"]; !ok {
		t.Fatalf("missing analyst_handoff: %#v", prior)
	}
	if _, ok := prior["analyst_working_memory"]; !ok {
		t.Fatalf("missing analyst_working_memory: %#v", prior)
	}
}

func TestRunWorkflowWithoutHandoffStillSucceeds(t *testing.T) {
	stubs := stubResponses{
		analyst:   analystResultJSON(), // result+trace only
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)
	_, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	prior := stubs.lastPortfolioPrior
	if _, ok := prior["analyst_handoff"]; ok {
		t.Fatalf("did not expect analyst_handoff: %#v", prior)
	}
}
```

**Note:** `setupRunnerEnv` currently takes `stubResponses` by value — change to `*stubResponses` (or close over a shared pointer) so the test can read `lastPortfolioPrior` after the run. Update existing call sites to `&stubResponses{...}` or keep a local `stubs := stubResponses{...}; setupRunnerEnv(t, &stubs)`.

- [ ] **Step 2: Run tests — expect fail**

```powershell
Set-Location services/api
go test ./internal/workflow/ -run "TestRunWorkflowInjectsAnalystHandoff|TestRunWorkflowWithoutHandoff" -count=1
```

Expected: FAIL (handoff not injected / prior not captured until stub updated).

- [ ] **Step 3: Implement injection in `runner.go`**

1. Extend `agentEnvelope` with `Handoff` / `WorkingMemory`.
2. After `prior[step.Name] = resultObj`, add:

```go
if step.Name == StepAnalyst {
	if len(envelope.Handoff) > 0 && string(envelope.Handoff) != "null" {
		var handoff any
		if err := json.Unmarshal(envelope.Handoff, &handoff); err != nil {
			return nil, false, fmt.Errorf("decode analyst handoff: %w", err)
		}
		prior["analyst_handoff"] = handoff
	}
	if len(envelope.WorkingMemory) > 0 && string(envelope.WorkingMemory) != "null" {
		var mem any
		if err := json.Unmarshal(envelope.WorkingMemory, &mem); err != nil {
			return nil, false, fmt.Errorf("decode analyst working_memory: %w", err)
		}
		prior["analyst_working_memory"] = mem
	}
}
```

3. Wire stub capture of `lastPortfolioPrior` + pointer-friendly `setupRunnerEnv`.

- [ ] **Step 4: Run tests — expect pass**

```powershell
go test ./internal/workflow/ -count=1
```

Expected: all PASS (including new tests).

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/workflow/runner.go services/api/internal/workflow/runner_test.go
git commit -m "feat(api): inject analyst handoff and working_memory into portfolio prior"
```

---

### Task 2: Portfolio — consume sibling keys in user context

**Files:**
- Modify: `services/agents/runtime/app/graphs/portfolio.py`
- Modify: `services/agents/runtime/tests/test_portfolio_graph.py`

**Interfaces:**
- `_user_message(req, baseline)` reads:
  - `prior.get("analyst_handoff")`
  - `prior.get("analyst_working_memory")`
- When present, append lines like:
  - `Analyst handoff: {json.dumps(...)}`
  - `Analyst working_memory: {json.dumps(...)}`
- When absent, omit those lines (existing tests must still pass)

- [ ] **Step 1: Write the failing test**

```python
def test_portfolio_user_message_includes_handoff_and_memory():
    from app.graphs.portfolio import _user_message

    req = {
        "trade_date": "2026-07-28",
        "watchlist": ["AAPL"],
        "account_snapshot": {},
        "risk_context": {},
        "prior_step_outputs": {
            "analyst": {"items": [{"symbol": "AAPL", "side": "buy"}]},
            "analyst_handoff": {
                "thesis_by_symbol": {"AAPL": {"summary": "bullish", "bias": "bull", "confidence": 0.8}},
                "open_questions": ["earnings?"],
            },
            "analyst_working_memory": {
                "notes": ["watched AAPL"],
                "evidence_refs": ["get_bars:AAPL"],
                "open_questions": ["earnings?"],
            },
        },
    }
    msg = _user_message(req, {"proposals": []})
    assert "analyst_handoff" in msg or "Analyst handoff" in msg
    assert "bullish" in msg
    assert "get_bars:AAPL" in msg
    assert "Analyst items" in msg
```

Also keep/extend an existing mock run with sibling keys in `_portfolio_request` to ensure `run_portfolio` still returns valid envelope:

```python
def test_portfolio_run_with_injected_handoff(monkeypatch, agent_run_request, mock_script_paths):
    monkeypatch.setenv("LLM_MODE", "mock")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(mock_script_paths["portfolio"]))
    from app.graphs.portfolio import run_portfolio
    req = _portfolio_request(agent_run_request)
    req["prior_step_outputs"]["analyst_handoff"] = {
        "thesis_by_symbol": {"AAPL": {"summary": "ok", "bias": "bull", "confidence": 0.7}},
        "evidence_refs": [],
    }
    req["prior_step_outputs"]["analyst_working_memory"] = {
        "notes": [],
        "evidence_refs": [],
        "open_questions": [],
    }
    out = run_portfolio(req)
    validate(out["result"], "portfolio_result")
    assert out["trace"]["stop_reason"]
```

- [ ] **Step 2: Run test — expect fail**

```powershell
$env:PYTHONPATH = "services/agents/common;services/agents/runtime"
python -m pytest services/agents/runtime/tests/test_portfolio_graph.py::test_portfolio_user_message_includes_handoff_and_memory -q
```

Expected: FAIL (message lacks handoff text).

- [ ] **Step 3: Implement `_user_message` update**

```python
def _user_message(req: dict[str, Any], baseline: dict[str, Any] | None) -> str:
    import json

    prior = req.get("prior_step_outputs") or {}
    analyst = prior.get("analyst") or {}
    parts = [
        f"Trade date: {req.get('trade_date')}",
        f"Watchlist: {req.get('watchlist')}",
        f"Analyst items: {analyst.get('items') or []}",
    ]
    handoff = prior.get("analyst_handoff")
    if handoff:
        parts.append(f"Analyst handoff: {json.dumps(handoff, ensure_ascii=False)}")
    memory = prior.get("analyst_working_memory")
    if memory:
        parts.append(f"Analyst working_memory: {json.dumps(memory, ensure_ascii=False)}")
    parts.extend(
        [
            f"Account: {req.get('account_snapshot')}",
            f"Risk context: {req.get('risk_context') or {}}",
            f"Baseline size_proposals: {(baseline or {}).get('proposals') or []}",
            "Plan sizing steps, call tools as needed (including size_proposals). "
            "Use analyst handoff/working_memory when present. "
            "Return portfolio_result JSON.",
        ]
    )
    return "\n".join(parts)
```

Prefer putting `import json` at module top if not already imported.

Optionally add one sentence to `SYSTEM_ACT`: `Prefer analyst handoff thesis/confidence when sizing; open_questions are informational.`

- [ ] **Step 4: Run portfolio tests — expect pass**

```powershell
python -m pytest services/agents/runtime/tests/test_portfolio_graph.py -q
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add services/agents/runtime/app/graphs/portfolio.py services/agents/runtime/tests/test_portfolio_graph.py
git commit -m "feat(portfolio): consume analyst handoff and working_memory in prompts"
```

---

### Task 3: Docs smoke

**Files:**
- Modify: `docs/product-overview.md`

- [ ] **Step 1: Add a short bullet under agent/runtime or Runs section**

Near the Analyst → Portfolio description (architecture or 3.x), add:

```markdown
- 同一 run 内，Go 将 Analyst 的 `handoff` / `working_memory` 以 `prior_step_outputs.analyst_handoff` 与 `analyst_working_memory` 注入 Portfolio；`analyst` 仍为 result（含 `items`）。缺手交信息不导致失败。
```

- [ ] **Step 2: Commit**

```bash
git add docs/product-overview.md
git commit -m "docs: note Analyst-to-Portfolio handoff injection"
```

---

## Self-review (plan vs spec)

| Spec requirement | Task |
|------------------|------|
| Parallel keys `analyst_handoff` / `analyst_working_memory` | Task 1 |
| `analyst` remains result-only | Task 1 tests |
| Missing handoff does not fail | Task 1 `TestRunWorkflowWithoutHandoffStillSucceeds` |
| Portfolio consumes in plan/act context | Task 2 `_user_message` |
| Risk/orders unchanged | No task (out of scope; leave alone) |
| product-overview note | Task 3 |
| Go workflow tests + Portfolio unit | Tasks 1–2 |

No placeholders. Key names consistent across Go/Python/docs.
