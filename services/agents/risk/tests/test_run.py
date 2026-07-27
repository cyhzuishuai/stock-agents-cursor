"""Tests for agent-risk POST /v1/run with LLM_MODE=mock."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app.main import (
    DEFAULT_NOTIONAL_REVIEW_THRESHOLD,
    app,
    advise_risk,
    run_risk,
)
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


def _prior_step_outputs(proposals: list[dict]) -> dict:
    return {"portfolio": {"proposals": proposals}}


def _proposal(*, symbol: str, side: str, notional: float) -> dict:
    cash_impact = -notional if side == "buy" else notional
    return {
        "symbol": symbol,
        "side": side,
        "qty": 10,
        "stop_loss": 100.0,
        "take_profit": 120.0,
        "estimated_notional": notional,
        "estimated_cash_impact": cash_impact,
    }


@pytest.fixture(autouse=True)
def _mock_llm_mode(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")


def test_mock_auto_when_notional_at_or_below_threshold():
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(
        [_proposal(symbol="AAPL", side="buy", notional=8000.0)]
    )

    result = run_risk(req)

    validate(result, "risk_advisory_result")
    item = result["items"][0]
    assert item["symbol"] == "AAPL"
    assert item["side"] == "buy"
    assert item["suggested_action"] == "auto"
    assert "size_ok" in item["flags"]
    assert item["scores"]["liquidity"] == pytest.approx(0.9)
    assert item["scores"]["volatility"] == pytest.approx(0.4)


def test_mock_review_when_notional_above_threshold():
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(
        [
            _proposal(symbol="AAPL", side="buy", notional=8000.01),
            _proposal(symbol="MSFT", side="sell", notional=5000.0),
        ]
    )

    result = advise_risk(req)

    validate(result, "risk_advisory_result")
    by_symbol = {item["symbol"]: item for item in result["items"]}
    assert by_symbol["AAPL"]["suggested_action"] == "review"
    assert "notional_high" in by_symbol["AAPL"]["flags"]
    assert by_symbol["MSFT"]["suggested_action"] == "auto"
    assert "size_ok" in by_symbol["MSFT"]["flags"]


def test_http_run_mock_returns_valid_advisory():
    client = TestClient(app)
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(
        [_proposal(symbol="AAPL", side="buy", notional=1000.0)]
    )

    response = client.post("/v1/run", json=req)

    assert response.status_code == 200
    result = response.json()
    validate(result, "risk_advisory_result")
    assert result["items"][0]["suggested_action"] in ("auto", "review")


def test_run_risk_uses_injected_llm_client(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")

    class FakeLLM:
        def complete_json(self, system: str, user: str, schema_name: str) -> dict:
            assert schema_name == "risk_advisory_result"
            assert "Proposals:" in user
            return {
                "items": [
                    {
                        "symbol": "AAPL",
                        "side": "buy",
                        "flags": ["manual_review"],
                        "scores": {"liquidity": 0.7, "volatility": 0.5},
                        "suggested_action": "review",
                    }
                ]
            }

    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(
        [_proposal(symbol="AAPL", side="buy", notional=1000.0)]
    )
    result = run_risk(req, llm_client=FakeLLM())

    validate(result, "risk_advisory_result")
    assert result["items"][0]["suggested_action"] == "review"
    assert result["items"][0]["flags"] == ["manual_review"]


def test_threshold_respects_env_override(monkeypatch):
    monkeypatch.setenv("NOTIONAL_REVIEW_THRESHOLD", "5000")
    req = _valid_request()
    req["prior_step_outputs"] = _prior_step_outputs(
        [_proposal(symbol="AAPL", side="buy", notional=5001.0)]
    )

    result = run_risk(req)

    assert result["items"][0]["suggested_action"] == "review"


def test_default_threshold_constant():
    assert DEFAULT_NOTIONAL_REVIEW_THRESHOLD == 8000.0


def test_healthz():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "agent-risk"}
