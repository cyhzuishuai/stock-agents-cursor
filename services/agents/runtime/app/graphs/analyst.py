"""AnalystGraph: evidence-gathering tool loop → analyst_result."""

from __future__ import annotations

from typing import Any

from stock_agents_common.llm_tools import ToolLLMClient
from stock_agents_common.tools import (
    RunContext,
    get_account_view,
    get_daily_bars,
    get_news,
    get_risk_context,
    web_search,
)

from app.graphs.loop import openai_tool_schema, run_tool_loop, web_search_enabled

SYSTEM_PROMPT = """You are an equity analyst agent with tools.
Gather evidence via tools (daily bars, news, web search, account/risk views), then return JSON:
{"items":[{"symbol","bias","confidence","thesis","side","urgency","rationale","evidence"?}], "warnings"?}
Cover every watchlist symbol. Weak evidence → hold + neutral + low confidence. Never invent conviction."""


def _tool_schemas() -> list[dict[str, Any]]:
    schemas = [
        openai_tool_schema(
            "get_daily_bars",
            "Fetch daily OHLCV bars for watchlist symbols (default lookback 20).",
            {
                "type": "object",
                "properties": {
                    "symbols": {"type": "array", "items": {"type": "string"}},
                    "lookback_days": {"type": "integer", "minimum": 1},
                },
            },
        ),
        openai_tool_schema(
            "get_news",
            "Fetch recent company news for a symbol (Finnhub).",
            {
                "type": "object",
                "required": ["symbol"],
                "properties": {
                    "symbol": {"type": "string"},
                    "from_date": {"type": "string"},
                    "to_date": {"type": "string"},
                },
            },
        ),
        openai_tool_schema(
            "get_account_view",
            "Return the injected Alpaca account snapshot (cash, equity, positions, open_orders).",
            {"type": "object", "properties": {}},
        ),
        openai_tool_schema(
            "get_risk_context",
            "Return injected risk_context (execution_mode and rule thresholds).",
            {"type": "object", "properties": {}},
        ),
    ]
    if web_search_enabled():
        schemas.append(
            openai_tool_schema(
                "web_search",
                "Web search for market/news context (degrades if unavailable).",
                {
                    "type": "object",
                    "required": ["query"],
                    "properties": {
                        "query": {"type": "string"},
                        "limit": {"type": "integer", "minimum": 1},
                    },
                },
            )
        )
    return schemas


def _tool_registry() -> dict[str, Any]:
    registry: dict[str, Any] = {
        "get_daily_bars": get_daily_bars,
        "get_news": get_news,
        "get_account_view": get_account_view,
        "get_risk_context": get_risk_context,
    }
    if web_search_enabled():
        registry["web_search"] = web_search
    return registry


def align_analyst_result(result: dict[str, Any], req: dict[str, Any]) -> dict[str, Any]:
    """Ensure one item per watchlist symbol; default hold/neutral for gaps."""
    watchlist: list[str] = list(req.get("watchlist") or [])
    items_by_symbol = {
        item["symbol"]: item for item in (result.get("items") or []) if isinstance(item, dict) and "symbol" in item
    }
    items: list[dict[str, Any]] = []
    warnings: list[str] = list(result.get("warnings") or [])

    for symbol in watchlist:
        if symbol in items_by_symbol:
            items.append(items_by_symbol[symbol])
            continue
        warnings.append(f"symbol_missing_from_llm:{symbol}")
        items.append(
            {
                "symbol": symbol,
                "bias": "neutral",
                "confidence": 0.3,
                "thesis": "No analyst item returned; defaulting to neutral hold.",
                "side": "hold",
                "urgency": "low",
                "rationale": "Missing from model output; default hold.",
            }
        )

    aligned: dict[str, Any] = {"items": items}
    if warnings:
        aligned["warnings"] = warnings
    return aligned


def _user_message(req: dict[str, Any]) -> str:
    return (
        f"Trade date: {req.get('trade_date')}\n"
        f"Watchlist: {req.get('watchlist')}\n"
        f"Account cash: {(req.get('account_snapshot') or {}).get('cash')}\n"
        f"Open orders: {(req.get('account_snapshot') or {}).get('open_orders') or []}\n"
        f"Risk context: {req.get('risk_context') or {}}\n"
        "Use tools as needed, then return analyst_result JSON covering every watchlist symbol."
    )


def run_analyst(
    req: dict[str, Any],
    *,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
) -> dict[str, Any]:
    return run_tool_loop(
        agent="analyst",
        req=req,
        system=SYSTEM_PROMPT,
        user_message=_user_message(req),
        tools_schema=_tool_schemas(),
        tool_registry=_tool_registry(),
        result_schema="analyst_result",
        align_result=align_analyst_result,
        llm_client=llm_client,
        ctx=ctx,
    )
