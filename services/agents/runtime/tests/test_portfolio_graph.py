from __future__ import annotations

import json

import pytest

from stock_agents_common.schemas import validate


def _portfolio_request(base: dict) -> dict:
    return {
        **base,
        "agent": "portfolio",
        "limits": {"max_tool_rounds": 8, "timeout_sec": 180},
        "prior_step_outputs": {
            "analyst": {
                "items": [
                    {
                        "symbol": "AAPL",
                        "bias": "bull",
                        "confidence": 0.72,
                        "thesis": "Upside",
                        "side": "buy",
                        "urgency": "normal",
                        "rationale": "Momentum",
                    },
                    {
                        "symbol": "MSFT",
                        "bias": "neutral",
                        "confidence": 0.4,
                        "thesis": "Wait",
                        "side": "hold",
                        "urgency": "low",
                        "rationale": "Covered",
                    },
                ]
            }
        },
    }


def test_portfolio_run_returns_result_and_trace(monkeypatch: pytest.MonkeyPatch, agent_run_request, mock_script_paths):
    monkeypatch.setenv("LLM_MODE", "mock")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(mock_script_paths["portfolio"]))

    from app.graphs.portfolio import run_portfolio

    req = _portfolio_request(agent_run_request)
    out = run_portfolio(req)

    assert "result" in out and "trace" in out
    assert out["trace"]["agent"] == "portfolio"
    assert out["trace"]["rounds"]
    assert out["trace"]["stop_reason"] in {"final", "max_rounds"}
    assert out["trace"].get("plan")
    assert any(e.get("type") == "plan" for e in (out["trace"].get("events") or []))
    validate(out["result"], "portfolio_result")
    validate(out, "agent_run_response")

    tool_names = [
        t.get("name")
        for round_entry in out["trace"]["rounds"]
        for t in (round_entry.get("tools") or [])
    ]
    assert "size_proposals" in tool_names


def test_portfolio_skips_holds_in_baseline(monkeypatch: pytest.MonkeyPatch, agent_run_request, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    # Force finalize after baseline tools so result uses size_proposals baseline
    script = tmp_path / "portfolio_baseline.json"
    script.write_text(
        json.dumps(
            {
                "plan": {
                    "steps": [
                        {"id": "s1", "title": "Account", "status": "pending"},
                        {"id": "s2", "title": "Closes", "status": "pending"},
                        {"id": "s3", "title": "Size", "status": "pending"},
                    ]
                },
                "reflect": [
                    {"decision": "mark_step_done", "step_id": "s1", "reason": "account"},
                    {"decision": "mark_step_done", "step_id": "s2", "reason": "closes"},
                    {"decision": "finalize", "reason": "use baseline"},
                ],
                "rounds": [
                    {"tool_calls": [{"id": "1", "name": "get_account_view", "args": {}}]},
                    {"tool_calls": [{"id": "2", "name": "get_last_closes", "args": {}}]},
                    {
                        "tool_calls": [
                            {
                                "id": "3",
                                "name": "size_proposals",
                                "args": {"closes": {"AAPL": 191.0, "MSFT": 420.0}},
                            }
                        ]
                    },
                ],
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(script))

    from app.graphs.portfolio import run_portfolio

    req = _portfolio_request(agent_run_request)
    req["limits"] = {"max_tool_rounds": 3}
    out = run_portfolio(req)

    validate(out["result"], "portfolio_result")
    assert out["trace"]["stop_reason"] in {"final", "max_rounds"}
    symbols = {p["symbol"] for p in out["result"]["proposals"]}
    assert "MSFT" not in symbols  # hold skipped
    assert "AAPL" in symbols
