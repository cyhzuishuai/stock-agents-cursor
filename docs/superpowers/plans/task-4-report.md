# Task 4 Report — LLM tool-calling client + mock mode

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Implemented `ToolLLMClient.complete_tools` with OpenAI-compatible `tools` / `tool_calls` support and `LLM_MODE=mock` scripted rounds. Existing `LLMClient.complete_json` is unchanged.

## Created

| File | Purpose |
|------|---------|
| `stock_agents_common/llm_tools.py` | `ToolLLMClient` — mock + real tool-calling |
| `tests/test_llm_tools_mock.py` | Mock script advance + real HTTP parse tests |
| `packages/contracts/fixtures/mock_tool_scripts/analyst.json` | Default analyst multi-round script |
| `packages/contracts/fixtures/mock_tool_scripts/portfolio.json` | Portfolio multi-round script (optional) |

## Round advancement (mock)

**Approach:** per-client instance integer `_round_index` (starts at 0).

- Each `complete_tools` call consumes `script["rounds"][_round_index]` then increments.
- **Not** derived from message length — create a new client or call `reset()` for a fresh run.
- Exhausted rounds raise `IndexError`.

Script resolution: `MOCK_TOOL_SCRIPT` env path → else default `fixtures/mock_tool_scripts/analyst.json`.

Round shapes:
- `{ "tool_calls": [{ "id", "name", "args" }] }` → `content=None`, normalized tool_calls
- `{ "content_json": {...} }` → JSON string in `content`, `tool_calls=None`

## Real path

- Env: `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL` (same defaults as `llm.py`)
- POST `{base}/chat/completions` with `tools` when schema non-empty
- Empty tools → `response_format: json_object` (finalize)
- Parses `message.tool_calls[].function.{name,arguments}` into `{id,name,args}`

## Verification

```text
cd services/agents/common
python -m pytest tests/test_llm_tools_mock.py -v   # 6 passed
python -m pytest -v                                 # 48 passed
```

## Commit

```
feat(agents): tool-calling LLM client with mock scripts
```

## Concerns

- Default mock script is analyst-only; portfolio graph (Task 5) should set `MOCK_TOOL_SCRIPT` to `portfolio.json` when needed.
- Real path does not schema-validate final JSON (runtime Task 5 will validate).
