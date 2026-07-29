"""agent-runtime: Analyst + Portfolio LangGraph tool-loops."""

from __future__ import annotations

from fastapi import FastAPI, HTTPException, Request
from jsonschema import ValidationError

from stock_agents_common.schemas import validate

from app.checkpoint import CheckpointUnavailableError
from app.graphs.analyst import run_analyst
from app.graphs.plan_loop import ThreadAlreadyCompleted, ThreadNotFound, ThreadNotPaused
from app.graphs.portfolio import run_portfolio
from app.threads import resume_by_thread, thread_status_payload

app = FastAPI(title="agent-runtime")


@app.get("/healthz")
def healthz() -> dict:
    return {"status": "ok", "agent": "agent-runtime"}


def _map_runtime_errors(exc: Exception) -> HTTPException | None:
    if isinstance(exc, CheckpointUnavailableError):
        return HTTPException(status_code=503, detail=str(exc))
    if isinstance(exc, ThreadAlreadyCompleted):
        return HTTPException(status_code=409, detail=str(exc))
    if isinstance(exc, ThreadNotFound):
        return HTTPException(status_code=404, detail=str(exc))
    if isinstance(exc, ThreadNotPaused):
        return HTTPException(status_code=409, detail=str(exc))
    return None


@app.post("/v1/run")
async def run(request: Request) -> dict:
    body = await request.json()
    try:
        validate(body, "agent_run_request")
    except ValidationError as exc:
        raise HTTPException(status_code=422, detail=exc.message) from exc

    agent = body.get("agent")
    try:
        if agent == "analyst":
            return run_analyst(body)
        if agent == "portfolio":
            return run_portfolio(body)
        raise HTTPException(status_code=422, detail=f"unsupported agent: {agent}")
    except ValidationError as exc:
        raise HTTPException(status_code=500, detail=f"invalid result schema: {exc.message}") from exc
    except ValueError as exc:
        mapped = _map_runtime_errors(exc)
        if mapped is not None:
            raise mapped from exc
        raise HTTPException(status_code=500, detail=str(exc)) from exc
    except Exception as exc:  # noqa: BLE001 — surface upstream LLM/tool errors to API
        mapped = _map_runtime_errors(exc)
        if mapped is not None:
            raise mapped from exc
        raise HTTPException(status_code=500, detail=f"{type(exc).__name__}: {exc}") from exc


@app.post("/v1/resume")
async def resume(request: Request) -> dict:
    body = await request.json()
    try:
        validate(body, "agent_resume_request")
    except ValidationError as exc:
        raise HTTPException(status_code=422, detail=exc.message) from exc

    try:
        return resume_by_thread(body["thread_id"], body["human_response"])
    except ValidationError as exc:
        raise HTTPException(status_code=500, detail=f"invalid result schema: {exc.message}") from exc
    except Exception as exc:  # noqa: BLE001 — map known runtime errors; else 500
        mapped = _map_runtime_errors(exc)
        if mapped is not None:
            raise mapped from exc
        raise HTTPException(status_code=500, detail=f"{type(exc).__name__}: {exc}") from exc


@app.get("/v1/threads/{thread_id:path}")
def get_thread(thread_id: str) -> dict:
    try:
        return thread_status_payload(thread_id)
    except Exception as exc:  # noqa: BLE001
        mapped = _map_runtime_errors(exc)
        if mapped is not None:
            raise mapped from exc
        raise HTTPException(status_code=500, detail=f"{type(exc).__name__}: {exc}") from exc
