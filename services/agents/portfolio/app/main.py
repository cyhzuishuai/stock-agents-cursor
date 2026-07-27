"""agent-portfolio: size decision intents into executable proposals."""

from __future__ import annotations

import math
import os
from typing import Any

from stock_agents_common.http_app import create_agent_app
from stock_agents_common.llm import LLMClient
from stock_agents_common.schemas import validate

SYSTEM_PROMPT = """You are a portfolio sizing agent. Given account cash, positions,
decision intents, and closes, refine trade proposals. Each proposal needs:
- symbol, side (buy|sell only), qty
- stop_loss and take_profit (±10% of close by default)
- estimated_notional (qty * close)
- estimated_cash_impact (negative for buy, positive for sell)

Skip hold intents. Respect cash and position constraints.
Return JSON with a "proposals" array."""

DEFAULT_MAX_NOTIONAL = 10_000.0
STOP_TAKE_PCT = 0.10


def _is_mock_mode() -> bool:
    return os.environ.get("LLM_MODE", "").strip().lower() == "mock"


def _max_notional() -> float:
    raw = os.environ.get("MAX_NOTIONAL", "").strip()
    if not raw:
        return DEFAULT_MAX_NOTIONAL
    try:
        return float(raw)
    except ValueError:
        return DEFAULT_MAX_NOTIONAL


def _closes_by_symbol(prior_step_outputs: dict) -> dict[str, float]:
    data = prior_step_outputs.get("data") or {}
    bars = data.get("bars") or []
    closes: dict[str, float] = {}
    for bar in bars:
        symbol = bar.get("symbol")
        if symbol is None or "close" not in bar:
            continue
        closes[symbol] = float(bar["close"])
    return closes


def _intents(prior_step_outputs: dict) -> list[dict]:
    decision = prior_step_outputs.get("decision") or {}
    return list(decision.get("intents") or [])


def _positions_by_symbol(account_snapshot: dict) -> dict[str, dict]:
    positions = account_snapshot.get("positions") or []
    return {pos["symbol"]: pos for pos in positions if "symbol" in pos}


def _proposal(
    *,
    symbol: str,
    side: str,
    qty: float,
    close: float,
) -> dict[str, Any]:
    notional = qty * close
    cash_impact = -notional if side == "buy" else notional
    return {
        "symbol": symbol,
        "side": side,
        "qty": qty,
        "stop_loss": close * (1.0 - STOP_TAKE_PCT),
        "take_profit": close * (1.0 + STOP_TAKE_PCT),
        "estimated_notional": notional,
        "estimated_cash_impact": cash_impact,
    }


def size_proposals(
    req: dict,
    *,
    max_notional: float | None = None,
) -> dict:
    """Deterministic sizing from account_snapshot + decision intents + closes."""
    prior = req.get("prior_step_outputs") or {}
    account = req["account_snapshot"]
    cash = float(account["cash"])
    closes = _closes_by_symbol(prior)
    positions = _positions_by_symbol(account)
    cap = DEFAULT_MAX_NOTIONAL if max_notional is None else max_notional

    proposals: list[dict] = []
    warnings: list[str] = []

    for intent in _intents(prior):
        symbol = intent.get("symbol")
        side = intent.get("side")
        if not symbol or not side:
            continue
        if side == "hold":
            continue

        close = closes.get(symbol)
        if close is None or close <= 0:
            warnings.append(f"missing_or_invalid_close:{symbol}")
            continue

        if side == "buy":
            budget = min(cap, cash * 0.05)
            qty = float(math.floor(budget / close))
            if qty <= 0:
                warnings.append(f"buy_qty_zero:{symbol}")
                continue
            proposals.append(_proposal(symbol=symbol, side="buy", qty=qty, close=close))
            continue

        if side == "sell":
            position = positions.get(symbol)
            position_qty = float(position["qty"]) if position else 0.0
            if position_qty <= 0:
                warnings.append(f"sell_no_position:{symbol}")
                continue
            qty = float(min(position_qty, max(1, math.floor(position_qty * 0.25))))
            proposals.append(_proposal(symbol=symbol, side="sell", qty=qty, close=close))
            continue

        warnings.append(f"unknown_side:{symbol}:{side}")

    result: dict = {"proposals": proposals}
    if warnings:
        result["warnings"] = warnings
    return result


def _build_user_prompt(req: dict, baseline: dict) -> str:
    account = req["account_snapshot"]
    prior = req.get("prior_step_outputs") or {}
    return (
        f"Trade date: {req['trade_date']}\n"
        f"Cash: {account['cash']}\n"
        f"Positions: {account.get('positions') or []}\n"
        f"Intents: {_intents(prior)}\n"
        f"Closes: {_closes_by_symbol(prior)}\n"
        f"Baseline proposals: {baseline.get('proposals') or []}\n"
        "Refine proposals if helpful; otherwise return the baseline."
    )


def run_portfolio(req: dict, llm_client: LLMClient | None = None) -> dict:
    baseline = size_proposals(req, max_notional=_max_notional())

    if _is_mock_mode():
        validate(baseline, "portfolio_result")
        return baseline

    client = llm_client or LLMClient()
    user_prompt = _build_user_prompt(req, baseline)
    refined = client.complete_json(SYSTEM_PROMPT, user_prompt, "portfolio_result")
    validate(refined, "portfolio_result")
    return refined


app = create_agent_app("agent-portfolio", run_portfolio)
