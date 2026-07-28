from __future__ import annotations

import httpx
import pytest

from stock_agents_common.marketdata.alpaca import AlpacaMarketDataProvider
from stock_agents_common.tools import (
    RunContext,
    get_account_view,
    get_daily_bars,
    get_last_closes,
    get_risk_context,
)
from stock_agents_common.trace import append_round, finalize_trace, new_trace, result_preview


def test_get_account_view_and_risk_context_from_request():
    ctx = RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL"],
            "account_snapshot": {
                "cash": 10000.0,
                "equity": 12000.0,
                "currency": "USD",
                "positions": [],
                "open_orders": [],
            },
            "risk_context": {
                "execution_mode": "auto",
                "rules": {"max_order_notional": 5000},
            },
        }
    )

    account = get_account_view(ctx)
    risk = get_risk_context(ctx)

    assert account == {"ok": True, "data": ctx.req["account_snapshot"]}
    assert risk == {"ok": True, "data": ctx.req["risk_context"]}


def test_get_daily_bars_default_lookback_20(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ALPACA_API_KEY", "k")
    monkeypatch.setenv("ALPACA_API_SECRET", "s")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "bars": {
                    "AAPL": [
                        {
                            "t": f"2026-07-{day:02d}T04:00:00Z",
                            "o": 100.0,
                            "h": 101.0,
                            "l": 99.0,
                            "c": 100.5,
                            "v": 1_000_000,
                        }
                        for day in range(1, 23)
                    ]
                }
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    provider = AlpacaMarketDataProvider(client=client)
    ctx = RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL"],
            "account_snapshot": {"cash": 1.0, "currency": "USD", "positions": []},
        },
        marketdata_provider=provider,
    )

    result = get_daily_bars(ctx)

    assert result["ok"] is True
    assert len(result["data"]["bars"]) == 20


def test_get_last_closes(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ALPACA_API_KEY", "k")
    monkeypatch.setenv("ALPACA_API_SECRET", "s")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "bars": {
                    "AAPL": [
                        {
                            "t": "2026-07-22T04:00:00Z",
                            "o": 100.0,
                            "h": 110.0,
                            "l": 99.0,
                            "c": 105.0,
                            "v": 1_000_000,
                        }
                    ]
                }
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    provider = AlpacaMarketDataProvider(client=client)
    ctx = RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL"],
            "account_snapshot": {"cash": 1.0, "currency": "USD", "positions": []},
        },
        marketdata_provider=provider,
    )

    result = get_last_closes(ctx, symbols=["AAPL"])

    assert result == {"ok": True, "data": {"closes": {"AAPL": 105.0}}}


def test_trace_helpers_build_rounds():
    trace = new_trace("analyst")
    assert trace["agent"] == "analyst"
    assert trace["rounds"] == []
    assert "started_at" in trace

    append_round(
        trace,
        {
            "i": 1,
            "llm": {"model": "mock", "latency_ms": 1},
            "assistant": {"content": "", "tool_calls": []},
            "tools": [],
        },
    )
    assert len(trace["rounds"]) == 1
    assert trace["rounds"][0]["i"] == 1

    finalize_trace(trace, "final")
    assert trace["stop_reason"] == "final"
    assert "ended_at" in trace


def test_result_preview_truncates():
    preview = result_preview({"x": "a" * 5000})
    assert len(preview) <= 2048
