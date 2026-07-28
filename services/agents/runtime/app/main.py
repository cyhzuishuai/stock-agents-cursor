"""agent-runtime: Analyst + Portfolio LangGraph tool-loops."""

from __future__ import annotations

from fastapi import FastAPI, HTTPException, Request
from jsonschema import ValidationError

from stock_agents_common.schemas import validate

from app.graphs.analyst import run_analyst
from app.graphs.portfolio import run_portfolio

app = FastAPI(title="agent-runtime")


@app.get("/healthz")
def healthz() -> dict:
    return {"status": "ok", "agent": "agent-runtime"}


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
        raise HTTPException(status_code=500, detail=str(exc)) from exc
