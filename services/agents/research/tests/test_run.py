"""Tests for agent-research POST /v1/run with LLM_MODE=mock."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app.main import VALID_BIASES, app, run_research
from stock_agents_common.llm import LLMClient
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


@pytest.fixture(autouse=True)
def _mock_llm_mode(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")


def test_run_research_mock_returns_valid_bias_per_symbol():
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
                },
                {
                    "symbol": "MSFT",
                    "trade_date": req["trade_date"],
                    "open": 420.0,
                    "high": 425.0,
                    "low": 418.0,
                    "close": 422.0,
                    "volume": 500_000.0,
                },
            ]
        }
    }

    result = run_research(req)

    validate(result, "research_result")
    assert {item["symbol"] for item in result["items"]} == set(req["watchlist"])
    for symbol in req["watchlist"]:
        item = next(item for item in result["items"] if item["symbol"] == symbol)
        assert item["bias"] in VALID_BIASES
        assert 0 <= item["confidence"] <= 1
        assert item["thesis"]


def test_http_run_mock_returns_valid_bias_per_symbol():
    client = TestClient(app)
    req = _valid_request()

    response = client.post("/v1/run", json=req)

    assert response.status_code == 200
    result = response.json()
    validate(result, "research_result")
    for symbol in req["watchlist"]:
        item = next(item for item in result["items"] if item["symbol"] == symbol)
        assert item["bias"] in VALID_BIASES


def test_run_research_uses_injected_llm_client():
    class FakeLLM:
        def complete_json(self, system: str, user: str, schema_name: str) -> dict:
            assert schema_name == "research_result"
            assert "AAPL" in user
            assert "MSFT" in user
            return {
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
            }

    req = _valid_request()
    result = run_research(req, llm_client=FakeLLM())

    validate(result, "research_result")
    assert result["items"][0]["bias"] == "bull"
    assert result["items"][1]["bias"] == "bear"


def test_healthz():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "agent-research"}
