# Task 2 Report: Multi-day bars on marketdata providers

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28

## Summary

Extended `MarketDataProvider.get_daily_bars` with optional `lookback_days: int = 1` keyword parameter. Alpaca and Free (Yahoo) providers now return up to N trading sessions per symbol ending on `trade_date`. Default `lookback_days=1` preserves existing single-bar behavior for all current callers.

## Changes

| File | Change |
|------|--------|
| `stock_agents_common/marketdata/base.py` | Protocol signature + docstring |
| `stock_agents_common/marketdata/alpaca.py` | Wider date window, slice last N bars per symbol |
| `stock_agents_common/marketdata/free.py` | Multi-bar Yahoo parse, slice last N sessions |
| `tests/test_marketdata_alpaca.py` | `test_alpaca_lookback_returns_multiple_bars` |
| `tests/test_marketdata_free.py` | `test_free_lookback_returns_multiple_bars` |

## Implementation notes

- **Alpaca:** When `lookback_days > 1`, start date backs off by `lookback_days + buffer` (min 7-day buffer for weekends). Filters bars `<= trade_date`, takes last N. For `lookback_days=1`, keeps original single-day API window and uses caller's `trade_date` on the bar dict.
- **Free/Yahoo:** Same buffer logic for `period1`. Parses all daily bars in range, filters to `<= trade_date`, returns last N. For `lookback_days=1`, still overrides `trade_date` on the returned bar (backward compatible with existing tests/callers).
- **Bar shape unchanged:** `{symbol, trade_date, open, high, low, close, volume}`.

## TDD

1. Added failing lookback tests (TypeError before implementation).
2. Implemented providers.
3. All tests pass.

## Test results

```
cd services/agents/common
python -m pytest tests/test_marketdata_alpaca.py tests/test_marketdata_free.py -v
# 8 passed

python -m pytest -v
# 17 passed (all common tests)
```

## Compatibility

- `agent-data` calls `get_daily_bars(symbols, trade_date)` — unchanged default.
- `FakeProvider` in data tests uses same 2-arg signature — still valid.

## Concerns / follow-ups

- Yahoo free provider `lookback_days=1` still maps `trade_date` from caller param, not bar timestamp (pre-existing behavior preserved).
- Weekend/holiday buffer is heuristic (`max(7, …)`); very long lookbacks (e.g. 60+) may need tuning if API returns fewer bars than requested.
- Task 3 `get_daily_bars` tool will pass `lookback_days=20` by default per spec.

## Commit

`feat(marketdata): multi-day daily bars lookback`
