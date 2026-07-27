"""Tests for agent-decision POST /v1/run with LLM_MODE=mock."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app.main import VALID_SIDES, app, run_decision
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
        "research": {
            "items": [
                {
                    "symbol": "AAPL",
                    "bias": "bull",
                    "confidence": 0.8,
                    "thesis": "Strong close.",
                },
                {
                    "symbol": "MSFT",
                    "bias": "bear",
                    "confidence": 0.6,
                    "thesis": "Weak volume.",
                },
            ]
        },
    }


@pytest.fixture(autouse=True)
def _mock_llm_mode(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")


def test_run_decision_mock_returns_valid_side_per_symbol():
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])

    result = run_decision(req)

    validate(result, "decision_result")
    assert {intent["symbol"] for intent in result["intents"]} == set(req["watchlist"])
    for symbol in req["watchlist"]:
        intent = next(intent for intent in result["intents"] if intent["symbol"] == symbol)
        assert intent["side"] in VALID_SIDES
        assert intent["urgency"]
        assert intent["rationale"]


def test_http_run_mock_returns_valid_side_per_symbol():
    client = TestClient(app)
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])

    response = client.post("/v1/run", json=req)

    assert response.status_code == 200
    result = response.json()
    validate(result, "decision_result")
    for symbol in req["watchlist"]:
        intent = next(intent for intent in result["intents"] if intent["symbol"] == symbol)
        assert intent["side"] in VALID_SIDES


def test_run_decision_uses_injected_llm_client():
    class FakeLLM:
        def complete_json(self, system: str, user: str, schema_name: str) -> dict:
            assert schema_name == "decision_result"
            assert "AAPL" in user
            assert "MSFT" in user
            assert "bias=bull" in user
            return {
                "intents": [
                    {
                        "symbol": "AAPL",
                        "side": "buy",
                        "urgency": "normal",
                        "rationale": "Bullish research.",
                    },
                    {
                        "symbol": "MSFT",
                        "side": "sell",
                        "urgency": "high",
                        "rationale": "Bearish research.",
                    },
                ]
            }

    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(req["trade_date"])
    result = run_decision(req, llm_client=FakeLLM())

    validate(result, "decision_result")
    assert result["intents"][0]["side"] == "buy"
    assert result["intents"][1]["side"] == "sell"


def test_healthz():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "agent-decision"}
