"""Deterministic portfolio sizing tool (ported from agent-portfolio)."""

from __future__ import annotations

import math
import os
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from stock_agents_common.tools import RunContext

DEFAULT_MAX_NOTIONAL = 10_000.0
STOP_TAKE_PCT = 0.10


def _max_notional_from_env() -> float:
    raw = os.environ.get("MAX_NOTIONAL", "").strip()
    if not raw:
        return DEFAULT_MAX_NOTIONAL
    try:
        return float(raw)
    except ValueError:
        return DEFAULT_MAX_NOTIONAL


def _positions_by_symbol(account_snapshot: dict) -> dict[str, dict]:
    positions = account_snapshot.get("positions") or []
    return {pos["symbol"]: pos for pos in positions if "symbol" in pos}


def _proposal(*, symbol: str, side: str, qty: float, close: float) -> dict[str, Any]:
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


def _resolve_items(ctx: RunContext, items: list[dict] | None) -> list[dict]:
    if items is not None:
        return list(items)
    prior = ctx.req.get("prior_step_outputs") or {}
    analyst = prior.get("analyst") or {}
    if isinstance(analyst, dict) and analyst.get("items") is not None:
        return list(analyst.get("items") or [])
    # Legacy decision intents fallback
    decision = prior.get("decision") or {}
    return list(decision.get("intents") or [])


def size_proposals(
    ctx: RunContext,
    *,
    items: list[dict] | None = None,
    closes: dict[str, float] | None = None,
    max_notional: float | None = None,
    **_args: Any,
) -> dict:
    """Size buy/sell/hold analyst items into executable proposals."""
    try:
        account = ctx.req.get("account_snapshot") or {}
        cash = float(account.get("cash") or 0.0)
        positions = _positions_by_symbol(account)
        cap = _max_notional_from_env() if max_notional is None else float(max_notional)
        close_map = {str(k): float(v) for k, v in (closes or {}).items()}
        resolved_items = _resolve_items(ctx, items)

        proposals: list[dict] = []
        warnings: list[str] = []

        for item in resolved_items:
            symbol = item.get("symbol")
            side = item.get("side")
            if not symbol or not side:
                continue
            if side == "hold":
                continue

            close = close_map.get(symbol)
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

        data: dict[str, Any] = {"proposals": proposals}
        if warnings:
            data["warnings"] = warnings
        return {"ok": True, "data": data}
    except Exception as exc:  # noqa: BLE001
        return {"ok": False, "error": str(exc)}
