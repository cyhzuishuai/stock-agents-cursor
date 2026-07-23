# Plan 05 — Python Common + Data Agent

> **Wave:** 1 (common) → 2 (data agent)  
> **Tracks:** T-PY-COMMON, T-PY-DATA  
> **Depends on:** Plan 01 schemas  
> **Parallel:** Wave1 with Plan 02 & 08.1; Wave2 data agent with Plan 03/04/06

**Goal:** Shared schema validation + market data adapter (`free` first, `alpaca` stub) + `agent-data` FastAPI service.

**Tech Stack:** Python 3.12, FastAPI, httpx, pydantic, pytest, yfinance (or stooq/yahoo HTTP).

---

### Task 05.1: Python package layout + schema loader

**Files:**
- Create: `services/agents/common/pyproject.toml` (or `requirements.txt`)
- Create: `services/agents/common/stock_agents_common/__init__.py`
- Create: `services/agents/common/stock_agents_common/schemas.py`
- Test: `services/agents/common/tests/test_schemas.py`

**Interfaces:**
- Produces: `validate(instance: dict, schema_name: str) -> None` loading from `packages/contracts/{schema_name}.schema.json`

- [ ] **Step 1: Failing test validating `agent_run_request` fixture path**

- [ ] **Step 2: Implement path resolution: from package walk up to repo `packages/contracts`**

- [ ] **Step 3: PASS + commit** `feat: python common schema validation`

---

### Task 05.2: Shared FastAPI run envelope

**Files:**
- Create: `services/agents/common/stock_agents_common/http_app.py`
- Test: `services/agents/common/tests/test_http_app.py`

**Interfaces:**

```python
def create_agent_app(name: str, handler: Callable[[dict], dict]) -> FastAPI:
    # POST /v1/run validates request schema, calls handler, returns dict
    # GET /healthz -> {"status":"ok","agent": name}
```

- [ ] **Step 1: Test with dummy handler**

- [ ] **Step 2: Implement**

- [ ] **Step 3: Commit** `feat: shared create_agent_app`

---

### Task 05.3: Market data adapter interface + free provider (mocked HTTP)

**Files:**
- Create: `services/agents/common/stock_agents_common/marketdata/base.py`
- Create: `services/agents/common/stock_agents_common/marketdata/free.py`
- Create: `services/agents/common/stock_agents_common/marketdata/alpaca.py`
- Create: `services/agents/common/stock_agents_common/marketdata/factory.py`
- Test: `services/agents/common/tests/test_marketdata_free.py`

**Interfaces:**

```python
class MarketDataProvider(Protocol):
    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        ...

def get_provider(name: str) -> MarketDataProvider: ...
```

- Free provider: fetch daily OHLCV; map to data_result bar fields; missing symbol → omit + caller adds warning
- Alpaca provider: raise `NotImplementedError("alpaca stub: set keys in later task")` OR return clear error dict — **must exist as class** selectable by factory

- [ ] **Step 1: Test free provider with httpx mock returning Yahoo-like JSON / CSV**

- [ ] **Step 2: Implement free provider (choose one concrete source; document in module docstring)**

- [ ] **Step 3: Factory reads `MARKET_DATA_PROVIDER`**

- [ ] **Step 4: Commit** `feat: market data adapter free provider`

---

### Task 05.4: agent-data service

**Files:**
- Create: `services/agents/data/app/main.py`
- Create: `services/agents/data/requirements.txt`
- Create: `services/agents/data/Dockerfile`
- Test: `services/agents/data/tests/test_run.py`

**Interfaces:**
- `POST /v1/run` → validates request, calls provider, returns payload matching `data_result.schema.json`
- If all symbols missing → still 200 with `bars:[]` and warning `all_symbols_missing` (Go workflow decides fail)

- [ ] **Step 1: Test handler with fake provider injected**

- [ ] **Step 2: Implement main using `create_agent_app`**

- [ ] **Step 3: Dockerfile EXPOSE 8001 CMD uvicorn**

- [ ] **Step 4: Commit** `feat: agent-data service`
