# Task 3 Report — Tools + trace helpers

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Implemented Task 3 from `2026-07-28-agent-runtime-tool-loop.md`: read-only agent tools (`get_daily_bars`, `get_news`, `web_search`, account/risk views, `get_last_closes`, `size_proposals`) and structured `trace` helpers. No LangGraph / runtime yet (Task 5).

## Created

| File | Purpose |
|------|---------|
| `stock_agents_common/trace.py` | `new_trace`, `append_round`, `finalize_trace`, `result_preview` (2KB truncate) |
| `stock_agents_common/tools/__init__.py` | `RunContext` + re-exports |
| `stock_agents_common/tools/bars.py` | `get_daily_bars` (lookback default 20), `get_last_closes` |
| `stock_agents_common/tools/news.py` | Finnhub company-news, top 3 |
| `stock_agents_common/tools/web_search.py` | Tavily; `WEB_SEARCH_ENABLED` default true |
| `stock_agents_common/tools/account.py` | `get_account_view` / `get_risk_context` from injected req |
| `stock_agents_common/tools/sizing.py` | Deterministic sizing from analyst items + closes |
| `tests/test_tools_*.py` | httpx MockTransport + sizing/trace unit tests |

## Behavior notes

- Tool contract: success `{"ok": True, "data": ...}` / failure `{"ok": False, "error": "..."}`.
- Missing `FINNHUB_API_KEY` → `missing_finnhub_api_key`; missing `WEB_SEARCH_API_KEY` → `missing_web_search_api_key`.
- `WEB_SEARCH_ENABLED`: unset/`""`/truthy ⇒ enabled; only `false`/`0`/`no` (case-insensitive) disables → `web_search_disabled`.
- `size_proposals` ports portfolio buy-budget / sell-25% / ±10% stops; skips `hold`; reads `prior_step_outputs.analyst.items` when `items` omitted.

## Verification

```text
cd services/agents/common
python -m pytest tests/test_tools_*.py tests/test_marketdata_*.py -v
```

**33 passed** (25 tool/trace + 8 marketdata).

## Commit

```
feat(agents): readonly tools and trace helpers
```

## Concerns

- `get_last_closes` lives in `bars.py` (not in the plan file map) so Portfolio Task 5 can import it without a new module.
- Serper provider is recognized as unsupported (`unsupported_web_search_provider`); only Tavily is implemented for now.
- News/web-search HTTP clients: inject via `RunContext.http_client` for tests; production will create a short-lived client per call when unset.
