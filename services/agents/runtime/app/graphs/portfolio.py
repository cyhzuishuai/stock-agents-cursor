"""PortfolioGraph: size proposals (+ short tool loop) → portfolio_result."""

from __future__ import annotations

from typing import Any

from stock_agents_common.llm_tools import ToolLLMClient
from stock_agents_common.tools import (
    RunContext,
    get_account_view,
    get_last_closes,
    get_risk_context,
    size_proposals,
)

from app.graphs.loop import execute_tool_call, openai_tool_schema, run_tool_loop

SYSTEM_PROMPT = """You are a portfolio sizing agent.
Use account/risk views, last closes, and size_proposals to produce executable proposals.
Skip hold intents. Never sell more than position qty. Respect cash constraints.
Return JSON: {"proposals":[{"symbol","side","qty","stop_loss?","take_profit?","estimated_notional","estimated_cash_impact","target_weight?"}], "warnings"?}
"""


def _tool_schemas() -> list[dict[str, Any]]:
    return [
        openai_tool_schema(
            "get_account_view",
            "Return the injected Alpaca account snapshot.",
            {"type": "object", "properties": {}},
        ),
        openai_tool_schema(
            "get_risk_context",
            "Return injected risk_context.",
            {"type": "object", "properties": {}},
        ),
        openai_tool_schema(
            "get_last_closes",
            "Fetch last close prices for watchlist symbols.",
            {
                "type": "object",
                "properties": {
                    "symbols": {"type": "array", "items": {"type": "string"}},
                },
            },
        ),
        openai_tool_schema(
            "size_proposals",
            "Deterministic sizing from analyst items (skips holds).",
            {
                "type": "object",
                "properties": {
                    "items": {"type": "array", "items": {"type": "object"}},
                    "closes": {
                        "type": "object",
                        "additionalProperties": {"type": "number"},
                    },
                    "max_notional": {"type": "number"},
                },
            },
        ),
    ]


def _tool_registry() -> dict[str, Any]:
    return {
        "get_account_view": get_account_view,
        "get_risk_context": get_risk_context,
        "get_last_closes": get_last_closes,
        "size_proposals": size_proposals,
    }


def align_portfolio_result(result: dict[str, Any], req: dict[str, Any]) -> dict[str, Any]:
    """Drop holds / invalid sides; keep schema-valid proposals only."""
    _ = req
    proposals: list[dict[str, Any]] = []
    raw_warnings = result.get("warnings")
    if isinstance(raw_warnings, str):
        warnings: list[str] = [raw_warnings] if raw_warnings.strip() else []
    elif isinstance(raw_warnings, list):
        warnings = [str(w) for w in raw_warnings if w is not None]
    else:
        warnings = []
    for prop in result.get("proposals") or []:
        if not isinstance(prop, dict):
            continue
        side = prop.get("side")
        if side == "hold":
            continue
        if side not in {"buy", "sell"}:
            warnings.append(f"skipped_invalid_side:{prop.get('symbol')}:{side}")
            continue
        proposals.append(prop)
    aligned: dict[str, Any] = {"proposals": proposals}
    if warnings:
        aligned["warnings"] = warnings
    return aligned


def _user_message(req: dict[str, Any], baseline: dict[str, Any] | None) -> str:
    prior = req.get("prior_step_outputs") or {}
    analyst = prior.get("analyst") or {}
    return (
        f"Trade date: {req.get('trade_date')}\n"
        f"Watchlist: {req.get('watchlist')}\n"
        f"Analyst items: {analyst.get('items') or []}\n"
        f"Account: {req.get('account_snapshot')}\n"
        f"Risk context: {req.get('risk_context') or {}}\n"
        f"Baseline size_proposals: {(baseline or {}).get('proposals') or []}\n"
        "Call tools as needed (including size_proposals). Return portfolio_result JSON."
    )


def _deterministic_baseline(ctx: RunContext, registry: dict[str, Any]) -> dict[str, Any] | None:
    """Run size_proposals once for a deterministic baseline (closes optional via tool loop)."""
    sized = execute_tool_call(
        name="size_proposals",
        args={},
        ctx=ctx,
        registry=registry,
    )
    if not sized["ok"]:
        return {"proposals": [], "warnings": [str(sized.get("error") or "size_proposals_failed")]}
    data = (sized["result"] or {}).get("data")
    if isinstance(data, dict) and "proposals" in data:
        return data
    return {"proposals": []}


def run_portfolio(
    req: dict[str, Any],
    *,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
) -> dict[str, Any]:
    run_ctx = ctx or RunContext(req=req)
    registry = _tool_registry()
    baseline = _deterministic_baseline(run_ctx, registry)

    return run_tool_loop(
        agent="portfolio",
        req=req,
        system=SYSTEM_PROMPT,
        user_message=_user_message(req, baseline),
        tools_schema=_tool_schemas(),
        tool_registry=registry,
        result_schema="portfolio_result",
        align_result=align_portfolio_result,
        baseline=baseline,
        llm_client=llm_client,
        ctx=run_ctx,
        ensure_size_proposals=True,
    )
