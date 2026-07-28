from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from stock_agents_common.schemas import validate


@pytest.fixture
def client(monkeypatch: pytest.MonkeyPatch, mock_script_paths):
    monkeypatch.setenv("LLM_MODE", "mock")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(mock_script_paths["analyst"]))
    from app.main import app

    return TestClient(app)


def test_healthz(client: TestClient):
    resp = client.get("/healthz")
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
    assert "agent" in body


def test_run_analyst_returns_200_with_result_and_trace(client: TestClient, agent_run_request):
    resp = client.post("/v1/run", json={**agent_run_request, "agent": "analyst"})
    assert resp.status_code == 200
    body = resp.json()
    assert "result" in body and "trace" in body
    assert body["trace"]["rounds"]
    assert body["trace"]["stop_reason"] in {"final", "max_rounds"}
    validate(body["result"], "analyst_result")
    validate(body, "agent_run_response")


def test_run_rejects_invalid_request(client: TestClient):
    resp = client.post("/v1/run", json={"run_id": "not-a-uuid"})
    assert resp.status_code == 422
