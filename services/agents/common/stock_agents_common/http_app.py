from __future__ import annotations

from collections.abc import Callable

from fastapi import FastAPI, HTTPException, Request
from jsonschema import ValidationError

from stock_agents_common.schemas import validate


def create_agent_app(name: str, handler: Callable[[dict], dict]) -> FastAPI:
    app = FastAPI()

    @app.get("/healthz")
    def healthz() -> dict:
        return {"status": "ok", "agent": name}

    @app.post("/v1/run")
    async def run(request: Request) -> dict:
        body = await request.json()
        try:
            validate(body, "agent_run_request")
        except ValidationError as exc:
            raise HTTPException(status_code=422, detail=exc.message) from exc
        return handler(body)

    return app
