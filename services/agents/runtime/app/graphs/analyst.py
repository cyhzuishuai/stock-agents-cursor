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
Rules:
- Cover every watchlist symbol.
- bias MUST be one of: bull|bear|neutral (not bullish/bearish).
- side MUST be one of: buy|sell|hold.
- urgency MUST be one of: low|normal|high.
- evidence MUST be an array of short strings (not one paragraph).
- warnings MUST be an array of strings (or omit).
- Weak evidence → hold + neutral + low confidence. Never invent conviction."""


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


def _as_str_list(value: Any) -> list[str]:
    """Coerce LLM quirks: string → [string]; list → str items; else []."""
    if value is None:
        return []
    if isinstance(value, str):
        text = value.strip()
        return [text] if text else []
    if isinstance(value, list):
        out: list[str] = []
        for item in value:
            if item is None:
                continue
            if isinstance(item, str):
                if item.strip():
                    out.append(item)
            else:
                out.append(str(item))
        return out
    return [str(value)]


_BIAS_ALIASES = {
    "bull": "bull",
    "bullish": "bull",
    "long": "bull",
    "positive": "bull",
    "bear": "bear",
    "bearish": "bear",
    "short": "bear",
    "negative": "bear",
    "neutral": "neutral",
    "flat": "neutral",
    "mixed": "neutral",
    "none": "neutral",
}
_SIDE_ALIASES = {
    "buy": "buy",
    "long": "buy",
    "sell": "sell",
    "short": "sell",
    "hold": "hold",
    "neutral": "hold",
    "none": "hold",
    "wait": "hold",
}
_URGENCY_ALIASES = {
    "low": "low",
    "normal": "normal",
    "medium": "normal",
    "med": "normal",
    "high": "high",
}


def _norm_enum(value: Any, aliases: dict[str, str], default: str) -> str:
    if value is None:
        return default
    key = str(value).strip().lower()
    return aliases.get(key, default)


def _coerce_analyst_item(item: dict[str, Any]) -> dict[str, Any]:
    allowed = ("symbol", "bias", "confidence", "thesis", "side", "urgency", "rationale", "evidence")
    cleaned = {k: item[k] for k in allowed if k in item}
    if "evidence" in cleaned:
        evidence = _as_str_list(cleaned.get("evidence"))
        if evidence:
            cleaned["evidence"] = evidence
        else:
            cleaned.pop("evidence", None)
    # Common live-LLM slips
    if isinstance(cleaned.get("confidence"), str):
        try:
            cleaned["confidence"] = float(cleaned["confidence"])
        except ValueError:
            cleaned["confidence"] = 0.3
    cleaned["bias"] = _norm_enum(cleaned.get("bias"), _BIAS_ALIASES, "neutral")
    cleaned["side"] = _norm_enum(cleaned.get("side"), _SIDE_ALIASES, "hold")
    cleaned["urgency"] = _norm_enum(cleaned.get("urgency"), _URGENCY_ALIASES, "normal")
    return cleaned


def align_analyst_result(result: dict[str, Any], req: dict[str, Any]) -> dict[str, Any]:
    """Ensure one item per watchlist symbol; default hold/neutral for gaps."""
    watchlist: list[str] = list(req.get("watchlist") or [])
    items_by_symbol = {
        item["symbol"]: _coerce_analyst_item(item)
        for item in (result.get("items") or [])
        if isinstance(item, dict) and "symbol" in item
    }
    items: list[dict[str, Any]] = []
    warnings: list[str] = _as_str_list(result.get("warnings"))

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
