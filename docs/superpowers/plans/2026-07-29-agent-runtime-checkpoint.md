# Agent Runtime Checkpoint + HITL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist LangGraph plan-loop state in SQLite so runs can crash-resume by `thread_id`, and pause only when the model calls `request_human_input`, with Go minimal interrupted/resume wiring.

**Architecture:** Compile plan-loop with `SqliteSaver`; in the tools node call `interrupt(human_request)` for `request_human_input`; HTTP returns a 200 interrupted envelope or full result. Go stores `interrupted` / `awaiting_agent_input` and proxies `POST /api/v1/runs/:id/agent-resume` to runtime `/v1/resume`, then continues the agent chain.

**Tech Stack:** Python FastAPI + LangGraph 1.x + `langgraph-checkpoint-sqlite`; Go Gin API + existing `workflow.Runner` / `agentsclient`

**Spec:** `docs/superpowers/specs/2026-07-29-agent-runtime-checkpoint-design.md`

## Global Constraints

- Checkpointer: LangGraph **SqliteSaver** via env `AGENT_CHECKPOINT_SQLITE_PATH` (default `/data/checkpoints.sqlite` in container)
- HITL only via tool `request_human_input` (not every reflect/finalize)
- `thread_id = "{run_id}:{agent}"` when omitted
- No checkpointer / SQLite unwritable → **503 fail closed**
- Completed thread re-`/v1/run` → **409** unless `force_new: true` (default false)
- V1: no Web UI; no Postgres checkpointer; no auto crash retry
- Do not merge agent HITL with trade `awaiting_approval`
- Keep non-HITL `/v1/run` envelope compatible (`result` + `trace`)

## File map

| File | Responsibility |
|------|----------------|
| `packages/contracts/agent_run_request.schema.json` | Add optional `thread_id`, `force_new` |
| `packages/contracts/agent_run_interrupted.schema.json` | Interrupted envelope schema |
| `packages/contracts/agent_resume_request.schema.json` | Resume body schema |
| `services/agents/common/stock_agents_common/tools/human_input.py` | Validate human-request args; schema helper |
| `services/agents/runtime/app/checkpoint.py` | SqliteSaver singleton + path setup |
| `services/agents/runtime/app/graphs/plan_loop.py` | checkpointer compile; interrupt in tools; invoke/resume helpers |
| `services/agents/runtime/app/graphs/analyst.py` / `portfolio.py` | Register human tool + prompt note |
| `services/agents/runtime/app/main.py` | `/v1/resume`, optional `/v1/threads/{id}`, interrupted 200 |
| `services/agents/runtime/requirements.txt` | `langgraph-checkpoint-sqlite` |
| `deploy/docker-compose.yml` / `env.example` / `README.md` | Volume + env |
| `services/api/internal/workflow/steps.go` | New statuses |
| `services/api/internal/agentsclient/client.go` | `Resume()` |
| `services/api/internal/workflow/runner.go` | Interrupted handling + resume continuation |
| `services/api/internal/httpserver/*` | `POST /runs/:id/agent-resume` |

---

### Task 1: Contracts — thread_id + interrupted/resume schemas

**Files:**
- Modify: `packages/contracts/agent_run_request.schema.json`
- Create: `packages/contracts/agent_run_interrupted.schema.json`
- Create: `packages/contracts/agent_resume_request.schema.json`
- Create: `packages/contracts/fixtures/agent_run_interrupted.valid.json`
- Create: `packages/contracts/fixtures/agent_resume_request.valid.json`
- Modify: `services/agents/common/tests/test_schemas.py` (or add assertions)

**Interfaces:**
- Consumes: existing draft-07 contract layout
- Produces: request may include `thread_id` (string, minLength 1) and `force_new` (boolean); interrupted schema requires `status` const `"interrupted"`, `thread_id`, `human_request` (object with required `question` string), `trace` object; resume schema requires `thread_id`, `human_response` object

- [ ] **Step 1: Write failing schema tests**

```python
def test_validate_agent_run_request_accepts_thread_id():
    raw = json.loads(Path(".../agent_run_request.valid.json").read_text())
    raw["thread_id"] = "1:analyst"
    raw["force_new"] = False
    validate(raw, "agent_run_request")

def test_validate_interrupted_envelope():
    validate(json.loads(Path(".../agent_run_interrupted.valid.json").read_text()), "agent_run_interrupted")

def test_validate_resume_request():
    validate(json.loads(Path(".../agent_resume_request.valid.json").read_text()), "agent_resume_request")
```

Adjust paths to match how `test_schemas.py` loads fixtures today.

- [ ] **Step 2: Run — expect FAIL** (unknown schema / additionalProperties)

```powershell
cd services/agents/common
python -m pytest tests/test_schemas.py -q -k "thread_id or interrupted or resume"
```

- [ ] **Step 3: Update schemas + fixtures**

In `agent_run_request.schema.json` properties add:

```json
"thread_id": { "type": "string", "minLength": 1 },
"force_new": { "type": "boolean" }
```

Create `agent_run_interrupted.schema.json`:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "agent_run_interrupted.schema.json",
  "type": "object",
  "required": ["status", "thread_id", "human_request", "trace"],
  "properties": {
    "status": { "const": "interrupted" },
    "thread_id": { "type": "string", "minLength": 1 },
    "human_request": {
      "type": "object",
      "required": ["question"],
      "properties": {
        "question": { "type": "string", "minLength": 1 },
        "context": { "type": "object" },
        "options": { "type": "array", "items": { "type": "string" } }
      },
      "additionalProperties": true
    },
    "trace": { "type": "object" }
  },
  "additionalProperties": true
}
```

Create `agent_resume_request.schema.json`:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "agent_resume_request.schema.json",
  "type": "object",
  "required": ["thread_id", "human_response"],
  "properties": {
    "thread_id": { "type": "string", "minLength": 1 },
    "human_response": {
      "type": "object",
      "properties": {
        "text": { "type": "string" },
        "data": { "type": "object" }
      },
      "additionalProperties": true
    }
  },
  "additionalProperties": false
}
```

Add matching fixtures under `packages/contracts/fixtures/`.

Ensure `stock_agents_common.schemas.validate` discovers new schema files the same way as existing ones (usually by stem name).

- [ ] **Step 4: Tests PASS**

```powershell
cd services/agents/common
python -m pytest tests/test_schemas.py -q
```

- [ ] **Step 5: Commit**

```powershell
git add packages/contracts services/agents/common/tests/test_schemas.py
git commit -m "feat(contracts): add thread_id and HITL interrupt/resume schemas"
```

---

### Task 2: Checkpoint helper + request_human_input tool

**Files:**
- Create: `services/agents/runtime/app/checkpoint.py`
- Create: `services/agents/common/stock_agents_common/tools/human_input.py`
- Modify: `services/agents/common/stock_agents_common/tools/__init__.py`
- Modify: `services/agents/runtime/requirements.txt` (add `langgraph-checkpoint-sqlite>=2.0`)
- Create: `services/agents/runtime/tests/test_checkpoint.py`
- Create: `services/agents/common/tests/test_tools_human_input.py`

**Interfaces:**
- Consumes: env `AGENT_CHECKPOINT_SQLITE_PATH`
- Produces:
  - `get_checkpointer() -> BaseCheckpointSaver` (raises `CheckpointUnavailableError` if path cannot be created/opened)
  - `default_thread_id(run_id: str, agent: str) -> str` → `f"{run_id}:{agent}"`
  - `validate_human_input_args(args: dict) -> tuple[dict | None, str | None]` → `(human_request, error)`; missing/blank question → `(None, "question_required")`
  - Tool fn `request_human_input(ctx, **args) -> dict` returns `{"ok": False, "error": "..."}` when invalid; when valid returns `{"ok": True, "data": human_request, "interrupt": True}` (actual `interrupt()` happens in plan_loop, not inside this fn)

- [ ] **Step 1: Failing tests**

```python
# test_tools_human_input.py
def test_missing_question():
    req, err = validate_human_input_args({})
    assert req is None and err == "question_required"

def test_valid_question():
    req, err = validate_human_input_args({"question": "Approve thesis?"})
    assert err is None and req["question"] == "Approve thesis?"

# test_checkpoint.py
def test_get_checkpointer_creates_sqlite(tmp_path, monkeypatch):
    path = tmp_path / "ck.sqlite"
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(path))
    from app.checkpoint import get_checkpointer, reset_checkpointer_for_tests
    reset_checkpointer_for_tests()
    cp = get_checkpointer()
    assert cp is not None
    assert path.exists() or path.parent.exists()
```

- [ ] **Step 2: Run — expect FAIL**

```powershell
cd services/agents/common
python -m pytest tests/test_tools_human_input.py -q
cd ../runtime
pip install "langgraph-checkpoint-sqlite>=2.0" -q
python -m pytest tests/test_checkpoint.py -q
```

- [ ] **Step 3: Implement**

`human_input.py`:

```python
def validate_human_input_args(args: dict) -> tuple[dict | None, str | None]:
    q = (args or {}).get("question")
    if not isinstance(q, str) or not q.strip():
        return None, "question_required"
    out: dict = {"question": q.strip()}
    if "context" in (args or {}) and isinstance(args["context"], dict):
        out["context"] = args["context"]
    if "options" in (args or {}) and isinstance(args["options"], list):
        out["options"] = [str(x) for x in args["options"]]
    return out, None

def request_human_input(ctx, **args):
    req, err = validate_human_input_args(args)
    if err:
        return {"ok": False, "error": err}
    return {"ok": True, "data": req, "interrupt": True}
```

`checkpoint.py`:

```python
import os
from pathlib import Path
from langgraph.checkpoint.sqlite import SqliteSaver

_checkpointer = None

class CheckpointUnavailableError(RuntimeError):
    pass

def reset_checkpointer_for_tests() -> None:
    global _checkpointer
    _checkpointer = None

def default_thread_id(run_id: str, agent: str) -> str:
    return f"{run_id}:{agent}"

def get_checkpointer():
    global _checkpointer
    if _checkpointer is not None:
        return _checkpointer
    path = os.environ.get("AGENT_CHECKPOINT_SQLITE_PATH", "/data/checkpoints.sqlite")
    try:
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        # SqliteSaver.from_conn_string may be a context manager in some versions —
        # keep a long-lived connection; follow installed package docs.
        _checkpointer = SqliteSaver.from_conn_string(path)
        if hasattr(_checkpointer, "__enter__"):
            _checkpointer = _checkpointer.__enter__()
        return _checkpointer
    except Exception as exc:
        raise CheckpointUnavailableError(str(exc)) from exc
```

Add `langgraph-checkpoint-sqlite>=2.0` to `requirements.txt`. Export `request_human_input` from tools `__init__.py`.

- [ ] **Step 4: Tests PASS**

```powershell
cd services/agents/common
python -m pytest tests/test_tools_human_input.py -q
cd ../runtime
python -m pytest tests/test_checkpoint.py -q
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/common services/agents/runtime/app/checkpoint.py services/agents/runtime/requirements.txt services/agents/runtime/tests/test_checkpoint.py
git commit -m "feat(runtime): add SqliteSaver helper and request_human_input tool"
```

---

### Task 3: Wire checkpointer + interrupt into plan_loop

**Files:**
- Modify: `services/agents/runtime/app/graphs/plan_loop.py`
- Modify: `services/agents/runtime/app/graphs/analyst.py`
- Modify: `services/agents/runtime/app/graphs/portfolio.py`
- Modify: `services/agents/runtime/tests/test_plan_loop.py`
- Create: `services/agents/runtime/tests/test_plan_loop_hitl.py`

**Interfaces:**
- Consumes: `get_checkpointer`, `default_thread_id`, `validate_human_input_args`, LangGraph `interrupt` / `Command` / `GraphInterrupt`
- Produces:
  - `run_plan_loop(..., thread_id: str | None = None, force_new: bool = False)` returns completed envelope **or** interrupted dict with `status=="interrupted"`
  - `resume_plan_loop(thread_id: str, human_response: dict, *, agent: str, ...deps...) -> dict`
  - On interrupt: `trace["stop_reason"] = "interrupted"`

**Tools node algorithm (two-pass):**

1. Partition `tool_calls` into `normal` and `human` (V1: interrupt on first human call after normals).
2. Execute all `normal` as today.
3. If any human call: validate args; on error treat as failed tool; on success `human_response = interrupt(human_req)` then append tool role message with `json.dumps(human_response)`.

```python
from langgraph.types import interrupt, Command
from langgraph.errors import GraphInterrupt
from app.checkpoint import get_checkpointer, default_thread_id
from stock_agents_common.tools.human_input import validate_human_input_args

compiled = graph.compile(checkpointer=get_checkpointer())
tid = thread_id or default_thread_id(str(req["run_id"]), agent)
config = {"configurable": {"thread_id": tid}}

# Before invoke: if not force_new and get_state shows completed (values set, next empty) → signal 409 to main
try:
    final_state = compiled.invoke(initial, config)
except GraphInterrupt as gi:
    human_request = _extract_interrupt_value(gi)
    return {
        "status": "interrupted",
        "thread_id": tid,
        "human_request": human_request,
        "trace": {**trace, "stop_reason": "interrupted", "agent": agent},
    }
```

`resume_plan_loop`: `compiled.invoke(Command(resume=human_response), config)` with same interrupt handling; on success reuse existing finalize/envelope path.

Register `request_human_input` in analyst/portfolio tool registries and OpenAI schemas; mention in act prompts: call only when confirmation or a critical fact is missing.

- [ ] **Step 1: Failing HITL test**

```python
def test_interrupt_then_resume(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(tmp_path / "c.sqlite"))
    from app.checkpoint import reset_checkpointer_for_tests
    reset_checkpointer_for_tests()
    # Fake LLM: plan → act with request_human_input → after resume reflect finalize
    out1 = run_plan_loop(..., thread_id="t1:analyst")
    assert out1["status"] == "interrupted"
    assert "question" in out1["human_request"]
    out2 = resume_plan_loop("t1:analyst", {"text": "yes"}, ...)
    assert "result" in out2 and out2.get("status") != "interrupted"
```

Also keep existing `test_plan_loop` green without human tool.

- [ ] **Step 2: Run — expect FAIL**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_loop_hitl.py tests/test_plan_loop.py -q
```

- [ ] **Step 3: Implement plan_loop + register tools**

- [ ] **Step 4: Tests PASS**

```powershell
cd services/agents/runtime
python -m pytest tests/test_plan_loop_hitl.py tests/test_plan_loop.py tests/test_analyst_graph.py tests/test_portfolio_graph.py -q
```

- [ ] **Step 5: Commit**

```powershell
git add services/agents/runtime
git commit -m "feat(runtime): checkpoint plan-loop and interrupt on request_human_input"
```

---

### Task 4: HTTP /v1/resume + thread status + 503/409

**Files:**
- Modify: `services/agents/runtime/app/main.py`
- Modify: `services/agents/runtime/tests/test_http.py`
- Create: `services/agents/runtime/tests/test_http_hitl.py`

**Interfaces:**
- Consumes: analyst/portfolio run + resume helpers
- Produces:
  - `POST /v1/run` → 200 completed | 200 interrupted | 409 completed-thread | 503 checkpoint unavailable
  - `POST /v1/resume` validated with `agent_resume_request` → same shapes; 404 unknown; 409 not paused
  - `GET /v1/threads/{thread_id}` → `{ "thread_id", "status": "paused"|"completed"|"unknown", "human_request"?: ... }`

Resume agent resolution: `thread_id.rsplit(":", 1)` → agent in `{analyst, portfolio}`.

```python
@app.post("/v1/resume")
async def resume(request: Request) -> dict:
    body = await request.json()
    validate(body, "agent_resume_request")
    try:
        return resume_by_thread(body["thread_id"], body["human_response"])
    except CheckpointUnavailableError as exc:
        raise HTTPException(503, detail=str(exc))
    except ThreadNotFound as exc:
        raise HTTPException(404, detail=str(exc))
    except ThreadNotPaused as exc:
        raise HTTPException(409, detail=str(exc))
```

- [ ] **Step 1: HTTP tests** including 503 when `get_checkpointer` raises `CheckpointUnavailableError`

```python
def test_run_interrupted_then_resume(client, tmp_path, monkeypatch, agent_run_request):
    ...
    r1 = client.post("/v1/run", json={**agent_run_request, "agent": "analyst", "thread_id": "u1:analyst"})
    assert r1.status_code == 200 and r1.json()["status"] == "interrupted"
    r2 = client.post("/v1/resume", json={"thread_id": "u1:analyst", "human_response": {"text": "ok"}})
    assert r2.status_code == 200 and "result" in r2.json()

def test_resume_unknown_thread_404(client):
    r = client.post("/v1/resume", json={"thread_id": "nope:analyst", "human_response": {"text": "x"}})
    assert r.status_code == 404
```

- [ ] **Step 2: Run — expect FAIL**

```powershell
cd services/agents/runtime
python -m pytest tests/test_http_hitl.py tests/test_http.py -q
```

- [ ] **Step 3: Implement main.py endpoints**

- [ ] **Step 4: Tests PASS + commit**

```powershell
git add services/agents/runtime/app/main.py services/agents/runtime/tests
git commit -m "feat(runtime): expose /v1/resume and interrupted HTTP envelopes"
```

---

### Task 5: Deploy — SQLite volume + env

**Files:**
- Modify: `deploy/docker-compose.yml` (`agent-runtime` volumes + env)
- Modify: `deploy/env.example`
- Modify: `deploy/README.md` (one short row for checkpoint path)

**Interfaces:**
- Produces: `AGENT_CHECKPOINT_SQLITE_PATH=/data/checkpoints.sqlite` and volume `agent_runtime_checkpoints:/data`

```yaml
# under agent-runtime:
environment:
  AGENT_CHECKPOINT_SQLITE_PATH: ${AGENT_CHECKPOINT_SQLITE_PATH:-/data/checkpoints.sqlite}
volumes:
  - agent_runtime_checkpoints:/data
```

Add top-level volume `agent_runtime_checkpoints:`.

- [ ] **Step 1: Edit compose + env.example + README**

- [ ] **Step 2: Sanity**

```powershell
docker compose -f deploy/docker-compose.yml config | Select-String checkpoint
```

- [ ] **Step 3: Commit**

```powershell
git add deploy/docker-compose.yml deploy/env.example deploy/README.md
git commit -m "chore(deploy): persist agent-runtime Sqlite checkpoints"
```

---

### Task 6: Go statuses + agentsclient.Resume

**Files:**
- Modify: `services/api/internal/workflow/steps.go`
- Modify: `services/api/internal/agentsclient/client.go`
- Create or modify: `services/api/internal/agentsclient/client_test.go`

**Interfaces:**
- Produces:
  - `StatusAwaitingAgentInput = "awaiting_agent_input"`
  - `StepStatusInterrupted = "interrupted"`
  - `func (c *Client) Resume(ctx context.Context, baseURL string, body any, timeout time.Duration) (json.RawMessage, error)` — POST `{base}/v1/resume`; HTTP 200 interrupted JSON is success

```go
func (c *Client) Resume(ctx context.Context, baseURL string, body any, timeout time.Duration) (json.RawMessage, error) {
    // mirror Call but url ends with "/v1/resume"
}
```

- [ ] **Step 1: Failing client test**

```go
func TestClientResumePostsToResumePath(t *testing.T) {
    var sawPath string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        sawPath = r.URL.Path
        w.Write([]byte(`{"result":{},"trace":{}}`))
    }))
    defer srv.Close()
    c := &Client{HTTP: srv.Client()}
    _, err := c.Resume(context.Background(), srv.URL, map[string]any{
        "thread_id": "1:analyst",
        "human_response": map[string]any{"text": "ok"},
    }, time.Second)
    if err != nil { t.Fatal(err) }
    if sawPath != "/v1/resume" { t.Fatalf("path %s", sawPath) }
}
```

- [ ] **Step 2: `go test` FAIL then implement**

```powershell
cd services/api
go test ./internal/agentsclient/ ./internal/workflow/ -count=1
```

- [ ] **Step 3: Commit**

```powershell
git add services/api/internal/workflow/steps.go services/api/internal/agentsclient
git commit -m "feat(api): add awaiting_agent_input status and Resume client"
```

---

### Task 7: Go runner interrupted + agent-resume API

**Files:**
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/httpserver/router.go`
- Modify: `services/api/internal/httpserver/handlers_runs.go` (or new `handlers_agent_resume.go`)
- Modify: `services/api/internal/workflow/runner_test.go`
- Modify: `services/api/internal/httpserver/api_smoke_test.go` if needed for route wiring

**Interfaces:**
- Extend `agentRunRequest` with `ThreadID string \`json:"thread_id,omitempty"\``
- Extend `agentEnvelope` with `Status`, `HumanRequest`, `ThreadID`
- After `Agents.Call`, if `status == "interrupted"`: persist step `interrupted`, set run `awaiting_agent_input`, **stop chain** without marking run `failed`
- `Runner.ResumeAgent(ctx, runID, agent, humanResponse)`:
  1. Load run; require `awaiting_agent_input` else conflict
  2. `thread_id := fmt.Sprintf("%d:%s", runID, agent)`
  3. `Agents.Resume(...)`
  4. Interrupted again → update step, keep status, return
  5. Completed → persist step `ok`, continue remaining `AgentChain()` then existing proposal/risk/fill path

**HTTP:**

```go
authed.POST("/runs/:id/agent-resume", api.PostAgentResume)
// body: { "agent", "human_response" }; 409 if not awaiting_agent_input
```

- [ ] **Step 1: Runner tests** — mock interrupted analyst; assert no portfolio step; resume then complete path

- [ ] **Step 2: `go test` FAIL**

```powershell
cd services/api
go test ./internal/workflow/ -count=1 -run Interrupted
```

- [ ] **Step 3: Implement runner + handler**

- [ ] **Step 4: Tests PASS**

```powershell
cd services/api
go test ./internal/workflow/ ./internal/httpserver/ ./internal/agentsclient/ -count=1
```

- [ ] **Step 5: Commit**

```powershell
git add services/api
git commit -m "feat(api): persist agent interrupts and resume agent chain"
```

---

### Task 8: Verification sweep

**Files:** none new (run suites); add missing 503 test only if Task 4 omitted it

- [ ] **Step 1: Python**

```powershell
cd services/agents/common
python -m pytest tests/ -q
cd ../runtime
python -m pytest tests/ -q
```

- [ ] **Step 2: Go**

```powershell
cd services/api
go test ./... -count=1
```

- [ ] **Step 3: Spec checklist**

| Criterion | Covered by |
|-----------|------------|
| SqliteSaver + thread_id | Tasks 2–3 |
| No HITL ≡ old behavior | Task 3 regression |
| Interrupted 200 envelope | Tasks 3–4 |
| `/v1/resume` | Task 4 |
| Go interrupted + agent-resume | Tasks 6–7 |
| 503 fail closed | Task 4 |

- [ ] **Step 4: Stop** (commit only if Step 3 added files)

---

## Spec coverage (self-review)

| Spec section | Task(s) |
|--------------|---------|
| SqliteSaver + env path | 2, 5 |
| `request_human_input` interrupt | 2, 3 |
| `/v1/run` interrupted + `/v1/resume` + threads GET | 4 |
| `thread_id` / `force_new` / 409 completed | 1, 3, 4 |
| Go statuses + Resume + agent-resume + continue chain | 6, 7 |
| Deploy volume | 5 |
| Tests in spec §7 | 3, 4, 7, 8 |
| Out of scope (UI, Postgres, auto-retry) | not scheduled |

**Type consistency:** `thread_id`, `human_request`, `human_response`, `status: "interrupted"`, `awaiting_agent_input`, `StepStatusInterrupted` used uniformly across Python contracts and Go constants.
