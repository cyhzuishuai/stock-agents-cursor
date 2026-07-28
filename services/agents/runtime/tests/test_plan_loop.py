"""Unit tests for plan → act → reflect LangGraph loop."""

from __future__ import annotations

import json

from stock_agents_common.schemas import validate
from stock_agents_common.tools import RunContext

from app.graphs.loop import max_rounds_for, openai_tool_schema
from app.graphs.plan_loop import run_plan_loop


class FakeClient:
    def __init__(self):
        self.n = 0

    def complete_plan(self, system, user):
        return {
            "plan_steps": [{"id": "s1", "title": "News", "status": "pending"}],
            "usage": {},
            "latency_ms": 1,
        }

    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {
                "content": None,
                "tool_calls": [{"id": "1", "name": "get_news", "args": {"symbol": "AAPL"}}],
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "content": json.dumps(
                {
                    "items": [
                        {
                            "symbol": "AAPL",
                            "bias": "bull",
                            "confidence": 0.7,
                            "thesis": "t",
                            "side": "buy",
                            "urgency": "normal",
                            "rationale": "r",
                        }
                    ],
                    "warnings": [],
                }
            ),
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }

    def complete_reflect(self, system, messages):
        if self.n == 1:
            return {
                "reflect": {"decision": "mark_step_done", "step_id": "s1", "reason": "got news"},
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "reflect": {"decision": "finalize", "reason": "done"},
            "usage": {},
            "latency_ms": 1,
        }


def _get_news(ctx, *, symbol: str, from_date=None, to_date=None):
    return {"ok": True, "data": {"headlines": []}}


def test_run_plan_loop_happy_path_emits_events_and_final_result():
    client = FakeClient()
    req = {
        "agent": "analyst",
        "trade_date": "2026-07-28",
        "watchlist": ["AAPL"],
        "account_snapshot": {"cash": 100000, "equity": 100000, "positions": [], "open_orders": []},
        "risk_context": {"execution_mode": "approval_required"},
        "limits": {"max_tool_rounds": 8},
    }

    out = run_plan_loop(
        agent="analyst",
        req=req,
        system_plan="plan",
        system_act="act",
        system_reflect="reflect",
        user_message="Analyze AAPL",
        tools_schema=[
            openai_tool_schema(
                "get_news",
                "Fetch news",
                {
                    "type": "object",
                    "required": ["symbol"],
                    "properties": {"symbol": {"type": "string"}},
                },
            )
        ],
        tool_registry={"get_news": _get_news},
        result_schema="analyst_result",
        llm_client=client,
        ctx=RunContext(req=req),
    )

    validate(out["result"], "analyst_result")
    validate(out, "agent_run_response")
    assert out["trace"]["events"]
    assert out["trace"]["stop_reason"] == "final"
    assert any(e.get("type") == "plan" for e in out["trace"]["events"])
    assert "get_news:True" in (out.get("working_memory") or {}).get("evidence_refs", []) or (
        "get_news:True" in (out["trace"].get("working_memory") or {}).get("evidence_refs", [])
    )


class RevisePatchClient:
    """revise_plan with list plan_patch must not call complete_plan again."""

    def __init__(self):
        self.plan_calls = 0
        self.n = 0
        self.plan_users: list[str] = []

    def complete_plan(self, system, user):
        self.plan_calls += 1
        self.plan_users.append(user)
        return {
            "plan_steps": [{"id": "s1", "title": "Old", "status": "pending"}],
            "usage": {},
            "latency_ms": 1,
        }

    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {
                "content": "need different evidence",
                "tool_calls": None,
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "content": json.dumps(
                {
                    "items": [
                        {
                            "symbol": "AAPL",
                            "bias": "neutral",
                            "confidence": 0.4,
                            "thesis": "t",
                            "side": "hold",
                            "urgency": "low",
                            "rationale": "r",
                        }
                    ],
                    "warnings": [],
                }
            ),
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }

    def complete_reflect(self, system, messages):
        if self.n == 1:
            return {
                "reflect": {
                    "decision": "revise_plan",
                    "reason": "switch step",
                    "plan_patch": [{"id": "s2", "title": "Bars", "status": "pending"}],
                },
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "reflect": {"decision": "finalize", "reason": "done"},
            "usage": {},
            "latency_ms": 1,
        }


def test_revise_plan_list_patch_skips_complete_plan_and_first_plan_is_clean():
    client = RevisePatchClient()
    req = {
        "agent": "analyst",
        "trade_date": "2026-07-28",
        "watchlist": ["AAPL"],
        "account_snapshot": {"cash": 100000, "equity": 100000, "positions": [], "open_orders": []},
        "risk_context": {"execution_mode": "approval_required"},
        "limits": {"max_tool_rounds": 8},
    }

    out = run_plan_loop(
        agent="analyst",
        req=req,
        system_plan="plan",
        system_act="act",
        system_reflect="reflect",
        user_message="Analyze AAPL",
        tools_schema=[],
        tool_registry={},
        result_schema="analyst_result",
        llm_client=client,
        ctx=RunContext(req=req),
    )

    assert client.plan_calls == 1
    assert client.plan_users[0] == "Analyze AAPL"
    assert "Prior plan:" not in client.plan_users[0]
    assert out["trace"]["stop_reason"] == "final"
    assert any(e.get("type") == "plan" and e.get("source") == "plan_patch" for e in out["trace"]["events"])
    validate(out, "agent_run_response")


class RepairOnceClient:
    """Invalid finalize content once, then valid JSON on same-model repair call."""

    def __init__(self):
        self.act_calls = 0
        self.repair_calls = 0

    def complete_plan(self, system, user):
        return {
            "plan_steps": [{"id": "s1", "title": "Decide", "status": "pending"}],
            "usage": {},
            "latency_ms": 1,
        }

    def complete_tools(self, system, messages, tools):
        # Same client for act + repair (never switch provider on parse fail).
        # Repair system prompt is distinct; act may also pass empty tools in unit tests.
        if "Fix JSON to schema" in system:
            self.repair_calls += 1
            return {
                "content": json.dumps(
                    {
                        "items": [
                            {
                                "symbol": "AAPL",
                                "bias": "bull",
                                "confidence": 0.7,
                                "thesis": "repaired",
                                "side": "buy",
                                "urgency": "normal",
                                "rationale": "fixed",
                            }
                        ],
                        "warnings": [],
                    }
                ),
                "tool_calls": None,
                "usage": {},
                "latency_ms": 1,
            }
        self.act_calls += 1
        return {
            "content": "this is not valid analyst JSON {{{",
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }

    def complete_reflect(self, system, messages):
        return {
            "reflect": {"decision": "finalize", "reason": "attempt final"},
            "usage": {},
            "latency_ms": 1,
        }


def test_finalize_same_model_repair_once_then_validates():
    client = RepairOnceClient()
    req = {
        "agent": "analyst",
        "trade_date": "2026-07-28",
        "watchlist": ["AAPL"],
        "account_snapshot": {"cash": 100000, "equity": 100000, "positions": [], "open_orders": []},
        "risk_context": {"execution_mode": "approval_required"},
        "limits": {"max_tool_rounds": 8},
    }

    out = run_plan_loop(
        agent="analyst",
        req=req,
        system_plan="plan",
        system_act="act",
        system_reflect="reflect",
        user_message="Analyze AAPL",
        tools_schema=[],
        tool_registry={},
        result_schema="analyst_result",
        llm_client=client,
        ctx=RunContext(req=req),
    )

    assert client.act_calls == 1
    assert client.repair_calls == 1
    validate(out["result"], "analyst_result")
    validate(out, "agent_run_response")
    assert out["result"]["items"][0]["thesis"] == "repaired"
    assert out["trace"]["stop_reason"] == "final"
    assert any(e.get("type") == "llm" and e.get("phase") == "repair" for e in out["trace"]["events"])


def test_max_rounds_for_portfolio_default_matches_analyst(monkeypatch):
    monkeypatch.delenv("MAX_TOOL_ROUNDS_PORTFOLIO", raising=False)
    monkeypatch.delenv("MAX_TOOL_ROUNDS_ANALYST", raising=False)
    assert max_rounds_for("portfolio", {}) == 8
    assert max_rounds_for("analyst", {}) == 8
