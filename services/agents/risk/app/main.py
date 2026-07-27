"""agent-risk: advisory flags and suggested_action for portfolio proposals."""

from __future__ import annotations

import os
from typing import Any

from stock_agents_common.http_app import create_agent_app
from stock_agents_common.llm import LLMClient
from stock_agents_common.schemas import validate

SYSTEM_PROMPT = """You are a risk advisory agent. For each portfolio proposal, assess
liquidity and volatility, set flags, and suggest auto or review.
Return JSON with an "items" array; each item needs symbol, side, flags,
scores (liquidity, volatility), and suggested_action (auto|review).
Advisory only — do not block trades."""

DEFAULT_NOTIONAL_REVIEW_THRESHOLD = 8000.0
DEFAULT_SCORES = {"liquidity": 0.9, "volatility": 0.4}


def _is_mock_mode() -> bool:
    return os.environ.get("LLM_MODE", "").strip().lower() == "mock"


def _notional_review_threshold() -> float:
    raw = os.environ.get("NOTIONAL_REVIEW_THRESHOLD", "").strip()
    if not raw:
        return DEFAULT_NOTIONAL_REVIEW_THRESHOLD
    try:
        return float(raw)
    except ValueError:
        return DEFAULT_NOTIONAL_REVIEW_THRESHOLD


def _proposals(prior_step_outputs: dict) -> list[dict]:
    portfolio = prior_step_outputs.get("portfolio") or {}
    return list(portfolio.get("proposals") or [])


def _advisory_item(proposal: dict, *, threshold: float) -> dict[str, Any]:
    notional = float(proposal["estimated_notional"])
    if notional > threshold:
        return {
            "symbol": proposal["symbol"],
            "side": proposal["side"],
            "flags": ["notional_high"],
            "scores": dict(DEFAULT_SCORES),
            "suggested_action": "review",
        }
    return {
        "symbol": proposal["symbol"],
        "side": proposal["side"],
        "flags": ["size_ok"],
        "scores": dict(DEFAULT_SCORES),
        "suggested_action": "auto",
    }


def advise_risk(
    req: dict,
    *,
    notional_review_threshold: float | None = None,
) -> dict:
    """Deterministic advisory from portfolio proposals (mock rule)."""
    proposals = _proposals(req.get("prior_step_outputs") or {})
    threshold = (
        DEFAULT_NOTIONAL_REVIEW_THRESHOLD
        if notional_review_threshold is None
        else notional_review_threshold
    )
    return {"items": [_advisory_item(proposal, threshold=threshold) for proposal in proposals]}


def _build_user_prompt(req: dict, baseline: dict) -> str:
    proposals = _proposals(req.get("prior_step_outputs") or {})
    return (
        f"Trade date: {req['trade_date']}\n"
        f"Proposals: {proposals}\n"
        f"Baseline advisory: {baseline.get('items') or []}\n"
        "Refine advisory if helpful; otherwise return the baseline."
    )


def run_risk(req: dict, llm_client: LLMClient | None = None) -> dict:
    baseline = advise_risk(req, notional_review_threshold=_notional_review_threshold())

    if _is_mock_mode():
        validate(baseline, "risk_advisory_result")
        return baseline

    client = llm_client or LLMClient()
    user_prompt = _build_user_prompt(req, baseline)
    refined = client.complete_json(SYSTEM_PROMPT, user_prompt, "risk_advisory_result")
    validate(refined, "risk_advisory_result")
    return refined


app = create_agent_app("agent-risk", run_risk)
