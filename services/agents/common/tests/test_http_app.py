import json
from pathlib import Path

from fastapi.testclient import TestClient

from stock_agents_common.http_app import create_agent_app


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


def test_healthz():
    app = create_agent_app("test-agent", lambda req: {"ok": True})
    client = TestClient(app)

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json() == {"status": "ok", "agent": "test-agent"}


def test_run_valid_request():
    received: dict = {}

    def handler(req: dict) -> dict:
        received.update(req)
        return {"result": "done"}

    app = create_agent_app("test-agent", handler)
    client = TestClient(app)
    body = _valid_request()

    response = client.post("/v1/run", json=body)

    assert response.status_code == 200
    assert response.json() == {"result": "done"}
    assert received == body


def test_run_invalid_request():
    app = create_agent_app("test-agent", lambda req: {"ok": True})
    client = TestClient(app)

    response = client.post("/v1/run", json={"bad": "data"})

    assert response.status_code == 422
