"""Tests for agent-data POST /v1/run with injected provider."""

from __future__ import annotations

import json
from pathlib import Path

from fastapi.testclient import TestClient

from app.main import app, run_data
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


class FakeProvider:
    def __init__(self, bars: list[dict] | None = None) -> None:
        self.bars = bars if bars is not None else []
        self.calls: list[tuple[list[str], str]] = []

    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        self.calls.append((symbols, trade_date))
        return self.bars


def test_run_data_returns_bars_and_validates_schema():
    trade_date = "2026-07-22"
    bars = [
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
    provider = FakeProvider(bars)
    req = _valid_request()

    result = run_data(req, provider=provider)

    validate(result, "data_result")
    assert result["bars"] == bars
    assert "warnings" not in result
    assert provider.calls == [(req["watchlist"], trade_date)]


def test_run_data_all_symbols_missing():
    provider = FakeProvider([])
    req = _valid_request()

    result = run_data(req, provider=provider)

    validate(result, "data_result")
    assert result == {"bars": [], "warnings": ["all_symbols_missing"]}


def test_run_data_partial_symbol_missing():
    trade_date = "2026-07-22"
    provider = FakeProvider(
        [
            {
                "symbol": "AAPL",
                "trade_date": trade_date,
                "open": 190.0,
                "high": 192.0,
                "low": 188.0,
                "close": 191.0,
                "volume": 1_000_000.0,
            }
        ]
    )
    req = _valid_request()

    result = run_data(req, provider=provider)

    validate(result, "data_result")
    assert len(result["bars"]) == 1
    assert result["warnings"] == ["symbol_missing:MSFT"]


def test_http_run_with_injected_provider(monkeypatch):
    trade_date = "2026-07-22"
    provider = FakeProvider(
        [
            {
                "symbol": "AAPL",
                "trade_date": trade_date,
                "open": 190.0,
                "high": 192.0,
                "low": 188.0,
                "close": 191.0,
                "volume": 1_000_000.0,
            }
        ]
    )
    monkeypatch.setattr("app.main.get_provider", lambda: provider)
    client = TestClient(app)

    response = client.post("/v1/run", json=_valid_request())

    assert response.status_code == 200
    validate(response.json(), "data_result")
    assert response.json()["bars"][0]["symbol"] == "AAPL"


def test_healthz():
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "agent-data"}
