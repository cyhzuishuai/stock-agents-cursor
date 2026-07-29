"""Thread status + resume routing for HTTP HITL."""

from __future__ import annotations

from typing import Any

from app.checkpoint import get_checkpointer
from app.graphs.analyst import resume_analyst
from app.graphs.plan_loop import ThreadNotFound, ThreadNotPaused
from app.graphs.portfolio import resume_portfolio

_SUPPORTED_AGENTS = frozenset({"analyst", "portfolio"})


def agent_from_thread_id(thread_id: str) -> str:
    parts = thread_id.rsplit(":", 1)
    if len(parts) != 2 or parts[1] not in _SUPPORTED_AGENTS:
        raise ThreadNotFound(f"unknown_thread:{thread_id}")
    return parts[1]


def _human_request_from_pending(pending_writes: list | None) -> dict[str, Any] | None:
    if not pending_writes:
        return None
    for _task_id, channel, value in pending_writes:
        if channel != "__interrupt__":
            continue
        seq = list(value) if isinstance(value, (list, tuple)) else [value]
        if not seq:
            continue
        first = seq[0]
        raw = getattr(first, "value", first)
        if isinstance(raw, dict):
            return raw
        return {"question": str(raw)}
    return None


def inspect_thread(thread_id: str) -> dict[str, Any]:
    """Return public status plus optional internal ``req`` for resume."""
    tup = get_checkpointer().get_tuple({"configurable": {"thread_id": thread_id}})
    if tup is None:
        return {"thread_id": thread_id, "status": "unknown"}

    values = tup.checkpoint.get("channel_values") or {}
    rb = values.get("runtime_bag") if isinstance(values.get("runtime_bag"), dict) else {}
    req = rb.get("req") if isinstance(rb.get("req"), dict) else None
    human = _human_request_from_pending(tup.pending_writes)

    if human is not None:
        out: dict[str, Any] = {
            "thread_id": thread_id,
            "status": "paused",
            "human_request": human,
        }
        if req is not None:
            out["req"] = req
        return out

    out = {"thread_id": thread_id, "status": "completed"}
    if req is not None:
        out["req"] = req
    return out


def thread_status_payload(thread_id: str) -> dict[str, Any]:
    info = inspect_thread(thread_id)
    payload = {"thread_id": info["thread_id"], "status": info["status"]}
    if info.get("human_request") is not None:
        payload["human_request"] = info["human_request"]
    return payload


def resume_by_thread(thread_id: str, human_response: dict[str, Any]) -> dict[str, Any]:
    agent = agent_from_thread_id(thread_id)
    info = inspect_thread(thread_id)
    if info["status"] == "unknown":
        raise ThreadNotFound(f"unknown_thread:{thread_id}")
    if info["status"] != "paused":
        raise ThreadNotPaused(f"thread_not_paused:{thread_id}")

    req = info.get("req")
    if not isinstance(req, dict):
        raise ThreadNotFound(f"unknown_thread:{thread_id}")

    if agent == "analyst":
        return resume_analyst(thread_id, human_response, req=req)
    return resume_portfolio(thread_id, human_response, req=req)
