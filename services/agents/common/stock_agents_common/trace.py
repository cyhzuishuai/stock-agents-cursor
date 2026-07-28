"""Structured tool-loop trace helpers."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any

PREVIEW_LIMIT = 2048


def _utcnow_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def new_trace(agent: str) -> dict[str, Any]:
    """Create an empty trace shell for ``agent`` (analyst|portfolio)."""
    return {
        "agent": agent,
        "started_at": _utcnow_iso(),
        "rounds": [],
    }


def append_round(trace: dict[str, Any], round_entry: dict[str, Any]) -> dict[str, Any]:
    """Append one round entry to ``trace['rounds']`` and return it."""
    rounds = trace.setdefault("rounds", [])
    if "i" not in round_entry:
        round_entry = {**round_entry, "i": len(rounds) + 1}
    rounds.append(round_entry)
    return round_entry


def finalize_trace(trace: dict[str, Any], stop_reason: str) -> dict[str, Any]:
    """Set ``ended_at`` and ``stop_reason`` on the trace."""
    trace["ended_at"] = _utcnow_iso()
    trace["stop_reason"] = stop_reason
    return trace


def result_preview(data: Any, *, limit: int = PREVIEW_LIMIT) -> str:
    """Serialize tool result for UI, truncated to ``limit`` characters."""
    try:
        text = json.dumps(data, ensure_ascii=False, default=str)
    except TypeError:
        text = str(data)
    if len(text) <= limit:
        return text
    return text[: max(0, limit - 3)] + "..."
