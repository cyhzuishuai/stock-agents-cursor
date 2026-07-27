"""Tests for agent-portfolio POST /v1/run with LLM_MODE=mock."""

from __future__ import annotations

import json
import math
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app.main import DEFAULT_MAX_NOTIONAL, app, run_portfolio, size_proposals
from stock_agents_common.schemas import validate


def _repo_root() -> Path:
    current = Path(__file__).resolve().parent
    for parent in [current, *current.parents]:
        if (parent / "packages" / "contracts").is_dir():
            return parent
    raise RuntimeError("repo root not found")


def _valid_request() -> dict:
    fixture_path = (
        _repo_root() / "packages" / "contracts" / "fixtures" / "agent_run_request.valid.json"
    )
    return json.loads(fixture_path.read_text(encoding="utf-8"))


def _prior_step_outputs(trade_date: str) -> dict:
    return {
        "data": {
            "bars": [
                {
                    "symbol": "AAPL",
                    "trade_date": trade_date,
                    "open": 190.0,
                    "high": 192.0,
                    "low": 188.0,
                    "close": 191.0,
                    "volume": 1_000_000.0,
                },
                {
                    "symbol": "MSFT",
                    "trade_date": trade_date,
                    "open": 420.0,
                    "high": 425.0,
                    "low": 418.0,
                    "close": 422.0,
                    "volume": 500_000.0,
                },
            ]
        },
        "decision": {
            "intents": [
                {
                    "symbol": "AAPL",
                    "side": "buy",
                    "urgency": "normal",
                    "rationale": "Bullish.",
                },
                {
                    "symbol": "MSFT",
                    "side": "hold",
                    "urgency": "low",
                    "rationale": "Neutral.",
                },
            ]
        },
    }


@pytest.fixture(autouse=True)
def _mock_llm_mode(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")


def test_mock_sizing_respects_cash_skips_hold_valid_schema():
    req = _valid_request()
    cash = float(req["account_snapshot"]["cash"])
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])

    result = run_portfolio(req)

    validate(result, "portfolio_result")
    symbols = {p["symbol"] for p in result["proposals"]}
    assert "MSFT" not in symbols  # hold skipped
    assert "AAPL" in symbols

    aapl = next(p for p in result["proposals"] if p["symbol"] == "AAPL")
    close = 191.0
    expected_qty = math.floor(min(DEFAULT_MAX_NOTIONAL, cash * 0.05) / close)
    assert aapl["side"] == "buy"
    assert aapl["qty"] == expected_qty
    assert aapl["estimated_notional"] == expected_qty * close
    assert aapl["estimated_cash_impact"] == -(expected_qty * close)
    assert aapl["stop_loss"] == pytest.approx(close * 0.9)
    assert aapl["take_profit"] == pytest.approx(close * 1.1)
    assert aapl["estimated_notional"] <= cash * 0.05 + 1e-9


def test_sell_sizing_from_position():
    req = _valid_request()
    req["prior_step_outputs"] = {
        "data": {
            "bars": [
                {
                    "symbol": "AAPL",
                    "trade_date": req["trade_date"],
                    "open": 190.0,
                    "high": 192.0,
                    "low": 188.0,
                    "close": 191.0,
                    "volume": 1_000_000.0,
                }
            ]
        },
        "decision": {
            "intents": [
                {
                    "symbol": "AAPL",
                    "side": "sell",
                    "urgency": "high",
                    "rationale": "Trim.",
                }
            ]
        },
    }

    result = size_proposals(req)
    validate(result, "portfolio_result")
    aapl = result["proposals"][0]
    position_qty = 10.0
    expected_qty = min(position_qty, max(1, math.floor(position_qty * 0.25)))
    assert aapl["side"] == "sell"
    assert aapl["qty"] == expected_qty
    assert aapl["estimated_cash_impact"] == expected_qty * 191.0


def test_http_run_mock_returns_valid_proposals():
    client = TestClient(app)
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])

    response = client.post("/v1/run", json=req)

    assert response.status_code == 200
    result = response.json()
    validate(result, "portfolio_result")
    assert all(p["side"] in ("buy", "sell") for p in result["proposals"])
    assert all(p["symbol"] != "MSFT" for p in result["proposals"])


def test_run_portfolio_uses_injected_llm_client(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")

    class FakeLLM:
        def complete_json(self, system: str, user: str, schema_name: str) -> dict:
            assert schema_name == "portfolio_result"
            assert "Baseline proposals" in user
            return {
                "proposals": [
                    {
                        "symbol": "AAPL",
                        "side": "buy",
                        "qty": 5,
                        "stop_loss": 171.9,
                        "take_profit": 210.1,
                        "estimated_notional": 955.0,
                        "estimated_cash_impact": -955.0,
                    }
                ]
            }

    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])
    result = run_portfolio(req, llm_client=FakeLLM())

    validate(result, "portfolio_result")
    assert result["proposals"][0]["qty"] == 5


def test_healthz():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "agent-portfolio"}
