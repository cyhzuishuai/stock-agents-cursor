"""HTTP HITL: interrupted /v1/run, /v1/resume, thread status, 503/409."""

from __future__ import annotations

import json

import pytest
from fastapi.testclient import TestClient

from stock_agents_common.schemas import validate


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
        },
        {
            "symbol": "MSFT",
            "bias": "neutral",
            "confidence": 0.4,
            "thesis": "range",
            "side": "hold",
            "urgency": "low",
            "rationale": "no edge",
        },
    ],
    "warnings": [],
}


class HitlFakeClient:
    """Shared across /v1/run then /v1/resume so mock indices survive."""

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
                        "args": {"question": "Buy AAPL?", "options": ["yes", "no"]},
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


@pytest.fixture
def hitl_client(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("LLM_MODE", "mock")
    fake = HitlFakeClient()
    monkeypatch.setattr("app.graphs.plan_loop.ToolLLMClient", lambda *a, **k: fake)
    from app.main import app

    return TestClient(app), fake


def test_run_interrupted_then_resume(hitl_client, agent_run_request):
    client, _fake = hitl_client
    r1 = client.post(
        "/v1/run",
        json={**agent_run_request, "agent": "analyst", "thread_id": "u1:analyst"},
    )
    assert r1.status_code == 200
    body1 = r1.json()
    assert body1["status"] == "interrupted"
    assert body1["thread_id"] == "u1:analyst"
    assert "question" in body1["human_request"]
    validate(body1, "agent_run_interrupted")

    r2 = client.post(
        "/v1/resume",
        json={"thread_id": "u1:analyst", "human_response": {"text": "ok"}},
    )
    assert r2.status_code == 200
    body2 = r2.json()
    assert "result" in body2
    assert body2.get("status") != "interrupted"
    validate(body2["result"], "analyst_result")
    validate(body2, "agent_run_response")


def test_resume_unknown_thread_404(hitl_client):
    client, _fake = hitl_client
    r = client.post(
        "/v1/resume",
        json={"thread_id": "nope:analyst", "human_response": {"text": "x"}},
    )
    assert r.status_code == 404


def test_thread_status_paused_then_completed(hitl_client, agent_run_request):
    client, _fake = hitl_client
    r1 = client.post(
        "/v1/run",
        json={**agent_run_request, "agent": "analyst", "thread_id": "u2:analyst"},
    )
    assert r1.status_code == 200 and r1.json()["status"] == "interrupted"

    st = client.get("/v1/threads/u2:analyst")
    assert st.status_code == 200
    body = st.json()
    assert body["thread_id"] == "u2:analyst"
    assert body["status"] == "paused"
    assert body["human_request"]["question"]

    r2 = client.post(
        "/v1/resume",
        json={"thread_id": "u2:analyst", "human_response": {"text": "ok"}},
    )
    assert r2.status_code == 200 and "result" in r2.json()

    st2 = client.get("/v1/threads/u2:analyst")
    assert st2.status_code == 200
    assert st2.json()["status"] == "completed"


def test_thread_status_unknown(hitl_client):
    client, _fake = hitl_client
    st = client.get("/v1/threads/missing:analyst")
    assert st.status_code == 200
    assert st.json() == {"thread_id": "missing:analyst", "status": "unknown"}


def test_resume_not_paused_409(hitl_client, agent_run_request):
    client, _fake = hitl_client
    r1 = client.post(
        "/v1/run",
        json={**agent_run_request, "agent": "analyst", "thread_id": "u3:analyst"},
    )
    assert r1.status_code == 200 and r1.json()["status"] == "interrupted"
    r2 = client.post(
        "/v1/resume",
        json={"thread_id": "u3:analyst", "human_response": {"text": "ok"}},
    )
    assert r2.status_code == 200 and "result" in r2.json()

    r3 = client.post(
        "/v1/resume",
        json={"thread_id": "u3:analyst", "human_response": {"text": "again"}},
    )
    assert r3.status_code == 409


def test_run_completed_thread_409(hitl_client, agent_run_request):
    client, _fake = hitl_client
    r1 = client.post(
        "/v1/run",
        json={**agent_run_request, "agent": "analyst", "thread_id": "u4:analyst"},
    )
    assert r1.status_code == 200 and r1.json()["status"] == "interrupted"
    r2 = client.post(
        "/v1/resume",
        json={"thread_id": "u4:analyst", "human_response": {"text": "ok"}},
    )
    assert r2.status_code == 200 and "result" in r2.json()

    r3 = client.post(
        "/v1/run",
        json={**agent_run_request, "agent": "analyst", "thread_id": "u4:analyst"},
    )
    assert r3.status_code == 409


def test_run_503_when_checkpoint_unavailable(monkeypatch, agent_run_request):
    monkeypatch.setenv("LLM_MODE", "mock")
    from app.checkpoint import CheckpointUnavailableError

    def _boom():
        raise CheckpointUnavailableError("sqlite unavailable")

    monkeypatch.setattr("app.graphs.plan_loop.get_checkpointer", _boom)
    from app.main import app

    client = TestClient(app)
    r = client.post("/v1/run", json={**agent_run_request, "agent": "analyst"})
    assert r.status_code == 503


def test_resume_rejects_invalid_body(hitl_client):
    client, _fake = hitl_client
    r = client.post("/v1/resume", json={"thread_id": "x:analyst"})
    assert r.status_code == 422
