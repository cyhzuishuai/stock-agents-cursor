"""agent-decision: per-symbol trade intents from research and bar context."""

from __future__ import annotations

from stock_agents_common.http_app import create_agent_app
from stock_agents_common.llm import LLMClient
from stock_agents_common.schemas import validate

SYSTEM_PROMPT = """You are a portfolio decision agent. For each symbol, review research bias and bar context, then produce one intent with:
- symbol: ticker
- side: buy, sell, or hold
- urgency: low, normal, or high
- rationale: one concise sentence

Return JSON with an "intents" array covering every symbol in the request."""

VALID_SIDES = frozenset({"buy", "sell", "hold"})


def _bars_by_symbol(prior_step_outputs: dict) -> dict[str, dict]:
    data = prior_step_outputs.get("data") or {}
    bars = data.get("bars") or []
    return {bar["symbol"]: bar for bar in bars if "symbol" in bar}


def _research_by_symbol(prior_step_outputs: dict) -> dict[str, dict]:
    research = prior_step_outputs.get("research") or {}
    items = research.get("items") or []
    return {item["symbol"]: item for item in items if "symbol" in item}


def _bar_summary(symbol: str, trade_date: str, bar: dict | None) -> str:
    if bar is None:
        return f"{symbol} ({trade_date}): no bar data"
    return (
        f"{symbol} ({bar.get('trade_date', trade_date)}): "
        f"open={bar['open']}, high={bar['high']}, low={bar['low']}, "
        f"close={bar['close']}, volume={bar['volume']}"
    )


def _research_summary(symbol: str, item: dict | None) -> str:
    if item is None:
        return f"{symbol}: no research item"
    return (
        f"{symbol}: bias={item['bias']}, confidence={item['confidence']}, "
        f"thesis={item['thesis']}"
    )


def _build_user_prompt(
    watchlist: list[str],
    trade_date: str,
    bars: dict[str, dict],
    research: dict[str, dict],
) -> str:
    lines = [f"Trade date: {trade_date}", "Research:"]
    lines.extend(_research_summary(symbol, research.get(symbol)) for symbol in watchlist)
    lines.append("Bar summaries:")
    lines.extend(_bar_summary(symbol, trade_date, bars.get(symbol)) for symbol in watchlist)
    lines.append(f"Watchlist symbols: {', '.join(watchlist)}")
    return "\n".join(lines)


def _align_intents_to_watchlist(result: dict, watchlist: list[str]) -> dict:
    intents_by_symbol = {
        intent["symbol"]: intent for intent in result.get("intents", []) if "symbol" in intent
    }
    intents: list[dict] = []
    warnings: list[str] = list(result.get("warnings") or [])

    for symbol in watchlist:
        if symbol in intents_by_symbol:
            intents.append(intents_by_symbol[symbol])
            continue
        warnings.append(f"symbol_missing_from_llm:{symbol}")
        intents.append(
            {
                "symbol": symbol,
                "side": "hold",
                "urgency": "normal",
                "rationale": "No LLM intent returned; defaulting to hold.",
            }
        )

    aligned: dict = {"intents": intents}
    if warnings:
        aligned["warnings"] = warnings
    return aligned


def run_decision(req: dict, llm_client: LLMClient | None = None) -> dict:
    watchlist: list[str] = req["watchlist"]
    trade_date: str = req["trade_date"]
    prior = req.get("prior_step_outputs") or {}
    bars = _bars_by_symbol(prior)
    research = _research_by_symbol(prior)

    client = llm_client or LLMClient()
    user_prompt = _build_user_prompt(watchlist, trade_date, bars, research)
    result = client.complete_json(SYSTEM_PROMPT, user_prompt, "decision_result")
    aligned = _align_intents_to_watchlist(result, watchlist)
    validate(aligned, "decision_result")
    return aligned


app = create_agent_app("agent-decision", run_decision)
