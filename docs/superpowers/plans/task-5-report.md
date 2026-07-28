# Task 5 Report — AnalystGraph + PortfolioGraph (`agent-runtime`)

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Added `services/agents/runtime` with FastAPI `POST /v1/run` (route on `agent`) and two LangGraph tool-loops that always return `{result, trace}` validated against contracts.

## Created

| Path | Role |
|------|------|
| `app/main.py` | FastAPI `/v1/run`, `/healthz` |
| `app/graphs/loop.py` | Shared LangGraph `call_model` ↔ `tools` loop + trace |
| `app/graphs/analyst.py` | Analyst tools + watchlist align (hold/neutral) |
| `app/graphs/portfolio.py` | Portfolio tools + deterministic `size_proposals` baseline |
| `requirements.txt` / `Dockerfile` / `pyproject.toml` | Deps, EXPOSE 8001, pytest pythonpath |
| `tests/test_*.py` | Graph + HTTP tests under `LLM_MODE=mock` |

## LangGraph API used

Installed **langgraph 1.2.x** (newer than plan’s `>=0.2` sketch). Wiring:

- `StateGraph(LoopState)` with TypedDict state
- Nodes: `call_model`, `tools`
- `set_entry_point("call_model")`
- `add_conditional_edges(call_model → tools | END)`
- `add_edge("tools", "call_model")` → `compile().invoke(state)`

No thin fallback needed after install succeeded (local pip required temporarily disabling broken IE proxy `127.0.0.1:7890`).

## Behavior notes

- Max rounds: request `limits.max_tool_rounds` else `MAX_TOOL_ROUNDS_ANALYST=8` / `MAX_TOOL_ROUNDS_PORTFOLIO=8`
- Analyst aligns missing watchlist symbols to hold/neutral
- Portfolio always runs deterministic `size_proposals` baseline (recorded in trace); skips holds; LLM may refine
- `WEB_SEARCH_ENABLED` default true; `false`/`0`/`no` omits tool binding

## Verification

```text
cd services/agents/runtime
pip install -e ../common -r requirements.txt
python -m pytest -v
# 7 passed
```

## Commit

```
feat(runtime): LangGraph analyst and portfolio tool loops
```
