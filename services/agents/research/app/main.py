"""agent-research: per-symbol bias and thesis from daily bar summaries."""

from __future__ import annotations

from stock_agents_common.http_app import create_agent_app
from stock_agents_common.llm import LLMClient
from stock_agents_common.schemas import validate

SYSTEM_PROMPT = """You are an equity research analyst. For each symbol, review its daily bar summary and produce one research item with:
- symbol: ticker
- bias: bull, bear, or neutral
- confidence: number from 0 to 1
- thesis: one concise sentence

Return JSON with an "items" array covering every symbol in the request."""

VALID_BIASES = frozenset({"bull", "bear", "neutral"})


def _bars_by_symbol(prior_step_outputs: dict) -> dict[str, dict]:
    data = prior_step_outputs.get("data") or {}
    bars = data.get("bars") or []
    return {bar["symbol"]: bar for bar in bars if "symbol" in bar}


def _bar_summary(symbol: str, trade_date: str, bar: dict | None) -> str:
    if bar is None:
        return f"{symbol} ({trade_date}): no bar data"
    return (
        f"{symbol} ({bar.get('trade_date', trade_date)}): "
        f"open={bar['open']}, high={bar['high']}, low={bar['low']}, "
        f"close={bar['close']}, volume={bar['volume']}"
    )


def _build_user_prompt(watchlist: list[str], trade_date: str, bars: dict[str, dict]) -> str:
    lines = [f"Trade date: {trade_date}", "Bar summaries:"]
    lines.extend(_bar_summary(symbol, trade_date, bars.get(symbol)) for symbol in watchlist)
    lines.append(f"Watchlist symbols: {', '.join(watchlist)}")
    return "\n".join(lines)


def _align_items_to_watchlist(result: dict, watchlist: list[str]) -> dict:
    items_by_symbol = {item["symbol"]: item for item in result.get("items", []) if "symbol" in item}
    items: list[dict] = []
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
                "confidence": 0.5,
                "thesis": "No LLM research item returned; defaulting to neutral.",
            }
        )

    aligned: dict = {"items": items}
    if warnings:
        aligned["warnings"] = warnings
    return aligned


def run_research(req: dict, llm_client: LLMClient | None = None) -> dict:
    watchlist: list[str] = req["watchlist"]
    trade_date: str = req["trade_date"]
    prior = req.get("prior_step_outputs") or {}
    bars = _bars_by_symbol(prior)

    client = llm_client or LLMClient()
    user_prompt = _build_user_prompt(watchlist, trade_date, bars)
    result = client.complete_json(SYSTEM_PROMPT, user_prompt, "research_result")
    aligned = _align_items_to_watchlist(result, watchlist)
    validate(aligned, "research_result")
    return aligned


app = create_agent_app("agent-research", run_research)
