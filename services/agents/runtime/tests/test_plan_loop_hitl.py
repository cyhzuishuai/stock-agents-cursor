"""HITL interrupt / resume tests for plan_loop."""

from __future__ import annotations

import json

from stock_agents_common.schemas import validate
from stock_agents_common.tools import RunContext

from app.checkpoint import reset_checkpointer_for_tests
from app.graphs.loop import openai_tool_schema
from app.graphs.plan_loop import resume_plan_loop, run_plan_loop


_ANALYST_RESULT = {
    "items": [
        {
            "symbol": "AAPL",
            "bias": "bull",
            "confidence": 0.7,
            "thesis": "confirmed",
            "side": "buy",
            "urgency": "normal",
            "rationale": "human said yes",
        }
    ],
    "warnings": [],
}


class HitlFakeClient:
    """plan → act(request_human_input) → after resume reflect mark_done → act JSON → finalize."""

    def __init__(self):
        self.n = 0

    def complete_plan(self, system, user):
        return {
            "plan_steps": [{"id": "s1", "title": "Confirm", "status": "pending"}],
            "usage": {},
            "latency_ms": 1,
        }

    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {
                "content": None,
                "tool_calls": [
                    {
                        "id": "h1",
                        "name": "request_human_input",
                        "args": {
                            "question": "Buy AAPL?",
                            "options": ["yes", "no"],
                        },
                    }
                ],
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "content": json.dumps(_ANALYST_RESULT),
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }

    def complete_reflect(self, system, messages):
        if self.n == 1:
            return {
                "reflect": {"decision": "mark_step_done", "step_id": "s1", "reason": "got human"},
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "reflect": {"decision": "finalize", "reason": "done"},
            "usage": {},
            "latency_ms": 1,
        }


def _base_req() -> dict:
    return {
        "run_id": "11111111-1111-1111-1111-111111111111",
        "agent": "analyst",
        "trade_date": "2026-07-28",
        "watchlist": ["AAPL"],
        "account_snapshot": {"cash": 100000, "equity": 100000, "positions": [], "open_orders": []},
        "risk_context": {"execution_mode": "approval_required"},
        "limits": {"max_tool_rounds": 8},
    }


def _hitl_kwargs(client: HitlFakeClient, req: dict) -> dict:
    return {
        "agent": "analyst",
        "req": req,
        "system_plan": "plan",
        "system_act": "act",
        "system_reflect": "reflect",
        "user_message": "Analyze AAPL",
        "tools_schema": [
            openai_tool_schema(
                "request_human_input",
                "Ask a human",
                {
                    "type": "object",
                    "required": ["question"],
                    "properties": {
                        "question": {"type": "string"},
                        "options": {"type": "array", "items": {"type": "string"}},
                    },
                },
            )
        ],
        "tool_registry": {},
        "result_schema": "analyst_result",
        "llm_client": client,
        "ctx": RunContext(req=req),
    }


def test_interrupt_then_resume(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(tmp_path / "c.sqlite"))
    reset_checkpointer_for_tests()

    client = HitlFakeClient()
    req = _base_req()
    kwargs = _hitl_kwargs(client, req)

    out1 = run_plan_loop(**kwargs, thread_id="t1:analyst")
    assert out1["status"] == "interrupted"
    assert "question" in out1["human_request"]
    assert out1["thread_id"] == "t1:analyst"
    assert out1["trace"]["stop_reason"] == "interrupted"
    validate(out1, "agent_run_interrupted")

    out2 = resume_plan_loop("t1:analyst", {"text": "yes"}, **kwargs)
    assert "result" in out2 and out2.get("status") != "interrupted"
    validate(out2["result"], "analyst_result")
    validate(out2, "agent_run_response")
    assert out2["result"]["items"][0]["thesis"] == "confirmed"


class SizeThenHumanClient:
    """Act once with size_proposals + request_human_input; after resume finalize JSON."""

    def __init__(self):
        self.n = 0

    def complete_plan(self, system, user):
        return {
            "plan_steps": [{"id": "s1", "title": "Size+confirm", "status": "pending"}],
            "usage": {},
            "latency_ms": 1,
        }

    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {
                "content": None,
                "tool_calls": [
                    {"id": "sp1", "name": "size_proposals", "args": {}},
                    {
                        "id": "h1",
                        "name": "request_human_input",
                        "args": {"question": "Approve sized proposals?"},
                    },
                ],
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "content": json.dumps(
                {
                    "proposals": [
                        {
                            "symbol": "AAPL",
                            "side": "buy",
                            "qty": 1,
                            "estimated_notional": 100.0,
                            "estimated_cash_impact": -100.0,
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
                "reflect": {"decision": "mark_step_done", "step_id": "s1", "reason": "ok"},
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "reflect": {"decision": "finalize", "reason": "done"},
            "usage": {},
            "latency_ms": 1,
        }


def test_resume_does_not_reinvoke_size_proposals(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(tmp_path / "c.sqlite"))
    reset_checkpointer_for_tests()

    calls = {"n": 0}

    def _size_proposals(ctx, **kwargs):
        calls["n"] += 1
        return {
            "ok": True,
            "data": {
                "proposals": [
                    {
                        "symbol": "AAPL",
                        "side": "buy",
                        "qty": 1,
                        "estimated_notional": 100.0,
                        "estimated_cash_impact": -100.0,
                    }
                ]
            },
        }

    client = SizeThenHumanClient()
    req = {
        **_base_req(),
        "agent": "portfolio",
    }
    kwargs = {
        "agent": "portfolio",
        "req": req,
        "system_plan": "plan",
        "system_act": "act",
        "system_reflect": "reflect",
        "user_message": "Size AAPL",
        "tools_schema": [
            openai_tool_schema("size_proposals", "size", {"type": "object", "properties": {}}),
            openai_tool_schema(
                "request_human_input",
                "Ask a human",
                {
                    "type": "object",
                    "required": ["question"],
                    "properties": {"question": {"type": "string"}},
                },
            ),
        ],
        "tool_registry": {"size_proposals": _size_proposals},
        "result_schema": "portfolio_result",
        "llm_client": client,
        "ctx": RunContext(req=req),
        "ensure_size_proposals": True,
    }

    out1 = run_plan_loop(**kwargs, thread_id="t2:portfolio")
    assert out1["status"] == "interrupted"
    assert calls["n"] == 1

    out2 = resume_plan_loop("t2:portfolio", {"text": "yes"}, **kwargs)
    assert "result" in out2 and out2.get("status") != "interrupted"
    assert calls["n"] == 1  # no re-exec on tools_human resume or ensure block
    validate(out2, "agent_run_response")


def test_completed_thread_raises_without_force_new(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(tmp_path / "c.sqlite"))
    reset_checkpointer_for_tests()

    from app.graphs.plan_loop import ThreadAlreadyCompleted

    class OnceClient:
        def __init__(self):
            self.n = 0

        def complete_plan(self, system, user):
            return {
                "plan_steps": [{"id": "s1", "title": "Decide", "status": "pending"}],
                "usage": {},
                "latency_ms": 1,
            }

        def complete_tools(self, system, messages, tools):
            self.n += 1
            return {
                "content": json.dumps(_ANALYST_RESULT),
                "tool_calls": None,
                "usage": {},
                "latency_ms": 1,
            }

        def complete_reflect(self, system, messages):
            return {
                "reflect": {"decision": "finalize", "reason": "done"},
                "usage": {},
                "latency_ms": 1,
            }

    client = OnceClient()
    req = _base_req()
    kwargs = {
        **_hitl_kwargs(client, req),
        "tools_schema": [],
        "tool_registry": {},
    }

    out1 = run_plan_loop(**kwargs, thread_id="t3:analyst")
    assert "result" in out1

    try:
        run_plan_loop(**kwargs, thread_id="t3:analyst")
        raise AssertionError("expected ThreadAlreadyCompleted")
    except ThreadAlreadyCompleted:
        pass

    out3 = run_plan_loop(**kwargs, thread_id="t3:analyst", force_new=True)
    assert "result" in out3


class InvalidThenValidHumanClient(HitlFakeClient):
    def complete_tools(self, system, messages, tools):
        self.n += 1
        if self.n == 1:
            return {
                "content": None,
                "tool_calls": [
                    {"id": "bad", "name": "request_human_input", "args": {}},
                    {
                        "id": "h1",
                        "name": "request_human_input",
                        "args": {"question": "Still need confirmation?"},
                    },
                ],
                "usage": {},
                "latency_ms": 1,
            }
        return {
            "content": json.dumps(_ANALYST_RESULT),
            "tool_calls": None,
            "usage": {},
            "latency_ms": 1,
        }


def test_invalid_human_then_valid_still_interrupts(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(tmp_path / "c.sqlite"))
    reset_checkpointer_for_tests()

    client = InvalidThenValidHumanClient()
    req = _base_req()
    kwargs = _hitl_kwargs(client, req)

    out1 = run_plan_loop(**kwargs, thread_id="t4:analyst")
    assert out1["status"] == "interrupted"
    assert out1["human_request"]["question"] == "Still need confirmation?"
