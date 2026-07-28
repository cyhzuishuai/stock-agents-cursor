from __future__ import annotations

import pytest

from stock_agents_common.schemas import validate


def test_analyst_run_returns_result_and_trace(monkeypatch: pytest.MonkeyPatch, agent_run_request, mock_script_paths):
    monkeypatch.setenv("LLM_MODE", "mock")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(mock_script_paths["analyst"]))
    monkeypatch.setenv("WEB_SEARCH_ENABLED", "true")

    from app.graphs.analyst import run_analyst

    req = {**agent_run_request, "agent": "analyst"}
    out = run_analyst(req)

    assert "result" in out and "trace" in out
    assert out["trace"]["agent"] == "analyst"
    assert out["trace"]["rounds"]
    assert out["trace"]["stop_reason"] in {"final", "max_rounds"}
    validate(out["result"], "analyst_result")
    validate(out, "agent_run_response")

    symbols = {item["symbol"] for item in out["result"]["items"]}
    assert symbols == set(req["watchlist"])


def test_analyst_aligns_missing_symbols_to_hold(monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = tmp_path / "analyst_partial.json"
    script.write_text(
        """
{
  "rounds": [
    {
      "content_json": {
        "items": [
          {
            "symbol": "AAPL",
            "bias": "bull",
            "confidence": 0.7,
            "thesis": "Momentum",
            "side": "buy",
            "urgency": "normal",
            "rationale": "Above MA"
          }
        ]
      }
    }
  ]
}
""".strip(),
        encoding="utf-8",
    )
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(script))

    from app.graphs.analyst import run_analyst

    req = {**agent_run_request, "agent": "analyst", "watchlist": ["AAPL", "MSFT"]}
    out = run_analyst(req)

    by_symbol = {item["symbol"]: item for item in out["result"]["items"]}
    assert by_symbol["AAPL"]["side"] == "buy"
    assert by_symbol["MSFT"]["side"] == "hold"
    assert by_symbol["MSFT"]["bias"] == "neutral"
    validate(out["result"], "analyst_result")
