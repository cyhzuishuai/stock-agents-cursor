from __future__ import annotations

import json

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


def test_analyst_accepts_think_wrapped_markdown_json(
    monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path
):
    monkeypatch.setenv("LLM_MODE", "mock")
    payload = {
        "items": [
            {
                "symbol": "AAPL",
                "bias": "bull",
                "confidence": 0.7,
                "thesis": "Momentum",
                "side": "buy",
                "urgency": "normal",
                "rationale": "Above MA",
            },
            {
                "symbol": "MSFT",
                "bias": "neutral",
                "confidence": 0.4,
                "thesis": "Wait",
                "side": "hold",
                "urgency": "low",
                "rationale": "Mixed",
            },
        ],
        "warnings": [],
    }
    wrapped = (
        "<think>I will output JSON after thinking.</think>\n"
        "```json\n"
        f"{json.dumps(payload)}\n"
        "```\n"
    )
    script = tmp_path / "analyst_think.json"
    script.write_text(json.dumps({"rounds": [{"content": wrapped}]}), encoding="utf-8")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(script))

    from app.graphs.analyst import run_analyst

    req = {**agent_run_request, "agent": "analyst", "watchlist": ["AAPL", "MSFT"]}
    out = run_analyst(req)

    validate(out["result"], "analyst_result")
    assert out["trace"]["stop_reason"] == "final"
    by_symbol = {item["symbol"]: item for item in out["result"]["items"]}
    assert by_symbol["AAPL"]["side"] == "buy"
    assert by_symbol["MSFT"]["side"] == "hold"


def test_analyst_max_rounds_falls_back_to_hold_defaults(
    monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path
):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = tmp_path / "analyst_exhaust.json"
    script.write_text(
        json.dumps(
            {
                "rounds": [
                    {
                        "tool_calls": [
                            {"id": "1", "name": "get_account_view", "args": {}},
                        ]
                    }
                ]
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(script))

    from app.graphs.analyst import run_analyst

    req = {
        **agent_run_request,
        "agent": "analyst",
        "watchlist": ["AAPL", "MSFT"],
        "limits": {"max_tool_rounds": 1, "timeout_sec": 180},
    }
    out = run_analyst(req)

    validate(out["result"], "analyst_result")
    assert out["trace"]["stop_reason"] == "max_rounds"
    by_symbol = {item["symbol"]: item for item in out["result"]["items"]}
    assert by_symbol["AAPL"]["side"] == "hold"
    assert by_symbol["AAPL"]["bias"] == "neutral"
    assert by_symbol["MSFT"]["side"] == "hold"
    assert by_symbol["MSFT"]["bias"] == "neutral"


def test_analyst_empty_non_json_falls_back_without_raising(
    monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path
):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = tmp_path / "analyst_garbage.json"
    script.write_text(
        json.dumps({"rounds": [{"content": "<think>no final answer</think> sorry"}]}),
        encoding="utf-8",
    )
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(script))

    from app.graphs.analyst import run_analyst

    req = {**agent_run_request, "agent": "analyst", "watchlist": ["AAPL", "MSFT"]}
    out = run_analyst(req)

    validate(out["result"], "analyst_result")
    assert out["trace"]["stop_reason"] == "max_rounds"
    assert {item["symbol"] for item in out["result"]["items"]} == {"AAPL", "MSFT"}
    assert all(item["side"] == "hold" for item in out["result"]["items"])
