from __future__ import annotations

import math

import pytest

from stock_agents_common.tools import RunContext, size_proposals

DEFAULT_MAX_NOTIONAL = 10_000.0


def _ctx(*, cash: float = 20_000.0, positions: list | None = None) -> RunContext:
    return RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL", "MSFT"],
            "account_snapshot": {
                "cash": cash,
                "currency": "USD",
                "positions": positions
                if positions is not None
                else [{"symbol": "AAPL", "qty": 10.0, "avg_cost": 150.0}],
            },
        }
    )


def test_size_proposals_buy_skips_hold():
    items = [
        {"symbol": "AAPL", "side": "buy", "bias": "bull", "confidence": 0.8},
        {"symbol": "MSFT", "side": "hold", "bias": "neutral", "confidence": 0.4},
    ]
    closes = {"AAPL": 191.0, "MSFT": 422.0}
    cash = 20_000.0

    result = size_proposals(_ctx(cash=cash), items=items, closes=closes)

    assert result["ok"] is True
    proposals = result["data"]["proposals"]
    symbols = {p["symbol"] for p in proposals}
    assert "MSFT" not in symbols
    assert "AAPL" in symbols

    aapl = next(p for p in proposals if p["symbol"] == "AAPL")
    expected_qty = float(math.floor(min(DEFAULT_MAX_NOTIONAL, cash * 0.05) / 191.0))
    assert aapl["side"] == "buy"
    assert aapl["qty"] == expected_qty
    assert aapl["estimated_notional"] == expected_qty * 191.0
    assert aapl["estimated_cash_impact"] == -(expected_qty * 191.0)
    assert aapl["stop_loss"] == pytest.approx(191.0 * 0.9)
    assert aapl["take_profit"] == pytest.approx(191.0 * 1.1)


def test_size_proposals_sell_from_position():
    items = [{"symbol": "AAPL", "side": "sell"}]
    closes = {"AAPL": 191.0}

    result = size_proposals(_ctx(), items=items, closes=closes)

    assert result["ok"] is True
    aapl = result["data"]["proposals"][0]
    position_qty = 10.0
    expected_qty = float(min(position_qty, max(1, math.floor(position_qty * 0.25))))
    assert aapl["side"] == "sell"
    assert aapl["qty"] == expected_qty
    assert aapl["estimated_cash_impact"] == expected_qty * 191.0


def test_size_proposals_uses_analyst_prior_when_items_omitted():
    ctx = _ctx()
    ctx.req["prior_step_outputs"] = {
        "analyst": {
            "items": [
                {"symbol": "AAPL", "side": "buy"},
                {"symbol": "MSFT", "side": "hold"},
            ]
        }
    }
    closes = {"AAPL": 191.0, "MSFT": 422.0}

    result = size_proposals(ctx, closes=closes)

    assert result["ok"] is True
    assert {p["symbol"] for p in result["data"]["proposals"]} == {"AAPL"}


def test_size_proposals_missing_close_warns():
    items = [{"symbol": "AAPL", "side": "buy"}]

    result = size_proposals(_ctx(), items=items, closes={})

    assert result["ok"] is True
    assert result["data"]["proposals"] == []
    assert "missing_or_invalid_close:AAPL" in result["data"]["warnings"]
