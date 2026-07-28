from __future__ import annotations

import json

import pytest

from stock_agents_common.schemas import validate


def _minimal_plan_script(*, rounds: list, reflect: list | None = None, steps: list | None = None) -> dict:
    """Build a mock script with plan/reflect so run_plan_loop can execute."""
    plan_steps = steps or [{"id": "s1", "title": "Gather evidence", "status": "pending"}]
    return {
        "plan": {"steps": plan_steps},
        "reflect": reflect or [{"decision": "finalize", "reason": "done"}],
        "rounds": rounds,
    }


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
    assert out["trace"].get("plan")
    assert any(e.get("type") == "plan" for e in (out["trace"].get("events") or []))
    assert "handoff" in out
    assert out["handoff"].get("thesis_by_symbol")
    event_types = [e.get("type") for e in (out["trace"].get("events") or [])]
    assert "handoff" in event_types
    assert "finalize" in event_types
    assert event_types.index("handoff") < event_types.index("finalize")
    assert isinstance(out["trace"].get("router"), dict)
    validate(out["result"], "analyst_result")
    validate(out["handoff"], "agent_handoff")
    validate(out, "agent_run_response")

    symbols = {item["symbol"] for item in out["result"]["items"]}
    assert symbols == set(req["watchlist"])


def test_analyst_aligns_missing_symbols_to_hold(monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = tmp_path / "analyst_partial.json"
    script.write_text(
        json.dumps(
            _minimal_plan_script(
                rounds=[
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
                                    "rationale": "Above MA",
                                }
                            ]
                        }
                    }
                ]
            )
        ),
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


def test_align_analyst_coerces_string_evidence_and_warnings():
    from app.graphs.analyst import align_analyst_result

    raw = {
        "items": [
            {
                "symbol": "AAPL",
                "bias": "neutral",
                "confidence": "0.4",
                "thesis": "Range-bound",
                "side": "hold",
                "urgency": "low",
                "rationale": "Thin volume",
                "evidence": "20-day close range 29.79-30.47. No news.",
            }
        ],
        "warnings": "sparse volume prints",
    }
    aligned = align_analyst_result(raw, {"watchlist": ["AAPL"]})
    validate(aligned, "analyst_result")
    assert aligned["items"][0]["evidence"] == ["20-day close range 29.79-30.47. No news."]
    assert aligned["items"][0]["confidence"] == 0.4
    assert aligned["warnings"] == ["sparse volume prints"]


def test_align_analyst_normalizes_bias_side_aliases():
    from app.graphs.analyst import align_analyst_result

    raw = {
        "items": [
            {
                "symbol": "AAPL",
                "bias": "bearish",
                "confidence": 0.6,
                "thesis": "Downtrend",
                "side": "short",
                "urgency": "medium",
                "rationale": "Lower highs",
            }
        ]
    }
    aligned = align_analyst_result(raw, {"watchlist": ["AAPL"]})
    validate(aligned, "analyst_result")
    assert aligned["items"][0]["bias"] == "bear"
    assert aligned["items"][0]["side"] == "sell"
    assert aligned["items"][0]["urgency"] == "normal"


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
    script.write_text(
        json.dumps(_minimal_plan_script(rounds=[{"content": wrapped}])),
        encoding="utf-8",
    )
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
            _minimal_plan_script(
                steps=[{"id": "s1", "title": "Account", "status": "pending"}],
                reflect=[{"decision": "continue", "reason": "keep going"}],
                rounds=[
                    {
                        "tool_calls": [
                            {"id": "1", "name": "get_account_view", "args": {}},
                        ]
                    }
                ],
            )
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
        json.dumps(
            _minimal_plan_script(
                rounds=[{"content": "<think>no final answer</think> sorry"}]
            )
        ),
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
