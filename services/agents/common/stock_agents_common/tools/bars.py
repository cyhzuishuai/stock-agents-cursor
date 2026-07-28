"""Daily bars and last-close tools via marketdata providers."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

from stock_agents_common.marketdata.factory import get_provider

if TYPE_CHECKING:
    from stock_agents_common.tools import RunContext

DEFAULT_LOOKBACK_DAYS = 20


def _provider(ctx: RunContext):
    if ctx.marketdata_provider is not None:
        return ctx.marketdata_provider
    return get_provider()


def get_daily_bars(
    ctx: RunContext,
    *,
    symbols: list[str] | None = None,
    lookback_days: int = DEFAULT_LOOKBACK_DAYS,
    **_args: Any,
) -> dict:
    try:
        resolved = list(symbols) if symbols is not None else list(ctx.req.get("watchlist") or [])
        trade_date = ctx.req.get("trade_date")
        if not trade_date:
            return {"ok": False, "error": "missing_trade_date"}
        if not resolved:
            return {"ok": False, "error": "missing_symbols"}
        bars = _provider(ctx).get_daily_bars(
            resolved,
            str(trade_date),
            lookback_days=int(lookback_days),
        )
        return {"ok": True, "data": {"bars": bars}}
    except Exception as exc:  # noqa: BLE001 — tools degrade to ok:false
        return {"ok": False, "error": str(exc)}


def get_last_closes(
    ctx: RunContext,
    *,
    symbols: list[str] | None = None,
    **_args: Any,
) -> dict:
    try:
        resolved = list(symbols) if symbols is not None else list(ctx.req.get("watchlist") or [])
        trade_date = ctx.req.get("trade_date")
        if not trade_date:
            return {"ok": False, "error": "missing_trade_date"}
        if not resolved:
            return {"ok": False, "error": "missing_symbols"}
        bars = _provider(ctx).get_daily_bars(resolved, str(trade_date), lookback_days=1)
        closes: dict[str, float] = {}
        for bar in bars:
            symbol = bar.get("symbol")
            if symbol is None or "close" not in bar:
                continue
            closes[str(symbol)] = float(bar["close"])
        return {"ok": True, "data": {"closes": closes}}
    except Exception as exc:  # noqa: BLE001
        return {"ok": False, "error": str(exc)}
