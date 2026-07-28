"""Shared tool helpers for analyst and portfolio agents.

Used by ``plan_loop`` for tool execution, OpenAI tool schemas, and round limits.
"""

from __future__ import annotations

import os
import time
from collections.abc import Callable
from typing import Any

from stock_agents_common.tools import RunContext
from stock_agents_common.trace import result_preview

ToolFn = Callable[..., dict]


def web_search_enabled() -> bool:
    raw = os.environ.get("WEB_SEARCH_ENABLED", "").strip().lower()
    if raw in {"false", "0", "no"}:
        return False
    return True


def max_rounds_for(agent: str, req: dict) -> int:
    limits = req.get("limits") or {}
    if limits.get("max_tool_rounds") is not None:
        return int(limits["max_tool_rounds"])
    env_key = "MAX_TOOL_ROUNDS_ANALYST" if agent == "analyst" else "MAX_TOOL_ROUNDS_PORTFOLIO"
    default = "8"
    return int(os.environ.get(env_key, default) or default)


def openai_tool_schema(name: str, description: str, parameters: dict[str, Any]) -> dict[str, Any]:
    return {
        "type": "function",
        "function": {
            "name": name,
            "description": description,
            "parameters": parameters,
        },
    }


def execute_tool_call(
    *,
    name: str,
    args: dict[str, Any],
    ctx: RunContext,
    registry: dict[str, ToolFn],
) -> dict[str, Any]:
    fn = registry.get(name)
    started = time.perf_counter()
    if fn is None:
        result: dict[str, Any] = {"ok": False, "error": f"unknown_tool:{name}"}
    else:
        try:
            result = fn(ctx, **(args or {}))
        except Exception as exc:  # noqa: BLE001 — tools degrade
            result = {"ok": False, "error": str(exc)}
    latency_ms = int((time.perf_counter() - started) * 1000)
    ok = bool(result.get("ok")) if isinstance(result, dict) else False
    error = None if ok else (result.get("error") if isinstance(result, dict) else "tool_error")
    return {
        "name": name,
        "ok": ok,
        "latency_ms": latency_ms,
        "result": result,
        "result_preview": result_preview(result),
        "error": error,
    }
