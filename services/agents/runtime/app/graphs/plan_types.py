"""Plan/reflect state helpers for the plan → act → reflect runtime."""

from __future__ import annotations

from typing import Any

_VALID_STEP_STATUSES = frozenset({"pending", "in_progress", "done", "skipped"})
_VALID_REFLECT_DECISIONS = frozenset(
    {"continue", "mark_step_done", "revise_plan", "finalize"}
)


def normalize_plan_steps(raw: Any) -> list[dict]:
    if not isinstance(raw, list):
        raise ValueError("plan steps must be a list")
    steps: list[dict] = []
    for index, item in enumerate(raw):
        if not isinstance(item, dict):
            raise ValueError("each plan step must be an object")
        title = item.get("title")
        if not title:
            raise ValueError("plan step requires title")
        step_id = item.get("id")
        if not step_id:
            step_id = f"s{index + 1}"
        status = item.get("status", "pending")
        if status not in _VALID_STEP_STATUSES:
            raise ValueError(f"invalid plan step status: {status}")
        step: dict[str, Any] = {
            "id": str(step_id),
            "title": str(title),
            "status": status,
        }
        tool_hint = item.get("tool_hint")
        if tool_hint is not None:
            step["tool_hint"] = str(tool_hint)
        steps.append(step)
    return steps


def empty_working_memory() -> dict:
    return {
        "notes": [],
        "evidence_refs": [],
        "open_questions": [],
    }


def append_evidence_ref(memory: dict, ref: str) -> None:
    memory.setdefault("evidence_refs", []).append(ref)


def normalize_reflect(raw: Any) -> dict:
    if not isinstance(raw, dict):
        raise ValueError("reflect must be an object")
    decision = raw.get("decision")
    if decision not in _VALID_REFLECT_DECISIONS:
        raise ValueError(f"invalid reflect decision: {decision}")
    result: dict[str, Any] = {"decision": decision}
    step_id = raw.get("step_id")
    if step_id is not None:
        result["step_id"] = str(step_id)
    reason = raw.get("reason")
    if reason is not None:
        result["reason"] = str(reason)
    plan_patch = raw.get("plan_patch")
    if plan_patch is not None:
        result["plan_patch"] = plan_patch
    return result
