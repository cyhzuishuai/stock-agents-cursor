# Plan 06 — Python LLM Agents (4-way parallel)

> **Wave:** 2–3  
> **Tracks:** T-PY-RESEARCH | T-PY-DECISION | T-PY-PORTFOLIO | T-PY-RISK  
> **Depends on:** Plan 05 Tasks 05.1–05.2 (common)  
> **Parallel:** Four subagents MAY run simultaneously — **one agent service each**. Do not edit `common/`.

**Goal:** Four FastAPI agents with LLM structured JSON (mockable via `LLM_MODE=mock`).

**Tech Stack:** FastAPI, httpx/OpenAI-compatible client, pydantic, pytest.

## Shared LLM client (each agent may copy thin wrapper OR import if added under common in Task 06.0)

### Task 06.0: Shared LLM JSON client (SERIAL within Plan 06 — do once before 06.1–06.4)

**Files:**
- Create: `services/agents/common/stock_agents_common/llm.py`
- Test: `services/agents/common/tests/test_llm_mock.py`

**Interfaces:**

```python
class LLMClient:
    def complete_json(self, system: str, user: str, schema_name: str) -> dict:
        """When LLM_MODE=mock, return fixture-driven dict; else call OpenAI-compatible API."""
```

- [ ] **Step 1: Test mock mode returns parsed JSON**

- [ ] **Step 2: Implement mock + real path (`LLM_API_KEY`, `LLM_BASE_URL`, model env `LLM_MODEL` default `gpt-4o-mini`)**

- [ ] **Step 3: Commit** `feat: shared llm json client with mock mode`

**Then launch 06.1–06.4 in parallel.**

---

### Task 06.1: agent-research

**Files (exclusive):** `services/agents/research/**`  
**Port:** 8002

**Interfaces:** Input `prior_step_outputs.data` optional; output `research_result.schema.json`.

- [ ] **Step 1: Test `/v1/run` with `LLM_MODE=mock` returns bias enum valid for each watchlist symbol**

- [ ] **Step 2: Implement handler prompt: for each symbol use bar summary → JSON items**

- [ ] **Step 3: Dockerfile + requirements**

- [ ] **Step 4: Commit** `feat: agent-research service`

---

### Task 06.2: agent-decision

**Files (exclusive):** `services/agents/decision/**`  
**Port:** 8003

**Interfaces:** Needs research (+ data) in `prior_step_outputs`; output `decision_result.schema.json`.

- [ ] **Step 1: Mock LLM test — intents sides in buy|sell|hold**

- [ ] **Step 2: Implement**

- [ ] **Step 3: Dockerfile**

- [ ] **Step 4: Commit** `feat: agent-decision service`

---

### Task 06.3: agent-portfolio

**Files (exclusive):** `services/agents/portfolio/**`  
**Port:** 8004

**Interfaces:** Uses account_snapshot + decision intents + closes from data; output `portfolio_result.schema.json`.

**Deterministic fallback when `LLM_MODE=mock`:**  
- `buy`: qty = floor(`min(max_notional, cash*0.05) / close`)  
- `sell`: qty = min(position_qty, max(1, floor(position_qty*0.25)))  
- attach default stop/take ±10%  

- [ ] **Step 1: Test mock sizing respects cash, skips `hold` intents, and produces proposals schema**

- [ ] **Step 2: Implement mock rules + optional LLM refine**

- [ ] **Step 3: Dockerfile**

- [ ] **Step 4: Commit** `feat: agent-portfolio service`

---

### Task 06.4: agent-risk (advisory)

**Files (exclusive):** `services/agents/risk/**`  
**Port:** 8005

**Interfaces:** Input portfolio proposals; output `risk_advisory_result.schema.json` with `suggested_action` auto|review.

**Mock rule:** if `estimated_notional > 8000` → `review` else `auto` (advisory only).

- [ ] **Step 1: Test mock flags**

- [ ] **Step 2: Implement**

- [ ] **Step 3: Dockerfile**

- [ ] **Step 4: Commit** `feat: agent-risk advisory service`
