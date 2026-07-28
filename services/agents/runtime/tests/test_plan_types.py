"""Unit tests for plan/reflect state helpers."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.graphs.plan_types import (
    append_evidence_ref,
    empty_working_memory,
    normalize_plan_steps,
    normalize_reflect,
)
from stock_agents_common.schemas import validate

ROOT = Path(__file__).resolve().parents[4]


def test_normalize_plan_steps_assigns_defaults():
    steps = normalize_plan_steps(
        [{"id": "s1", "title": "Fetch bars"}, {"title": "News only"}]
    )
    assert steps[0]["status"] == "pending"
    assert steps[0]["id"] == "s1"
    assert steps[1]["id"]  # auto id
    assert steps[1]["title"] == "News only"


def test_normalize_reflect_decisions():
    assert normalize_reflect({"decision": "finalize", "reason": "done"})["decision"] == "finalize"
    with pytest.raises(ValueError):
        normalize_reflect({"decision": "nope"})


def test_working_memory_evidence():
    mem = empty_working_memory()
    append_evidence_ref(mem, "get_daily_bars:AAPL")
    assert mem["evidence_refs"] == ["get_daily_bars:AAPL"]


def test_agent_handoff_fixture_validates():
    data = json.loads(
        (ROOT / "packages/contracts/fixtures/agent_handoff.valid.json").read_text(encoding="utf-8")
    )
    validate(data, "agent_handoff")
