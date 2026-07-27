"""agent-data: fetch daily OHLCV bars for the EOD watchlist."""

from __future__ import annotations

from stock_agents_common.http_app import create_agent_app
from stock_agents_common.marketdata.base import MarketDataProvider
from stock_agents_common.marketdata.factory import get_provider


def run_data(req: dict, provider: MarketDataProvider | None = None) -> dict:
    resolved = provider or get_provider()
    symbols: list[str] = req["watchlist"]
    trade_date: str = req["trade_date"]
    bars = resolved.get_daily_bars(symbols, trade_date)

    warnings: list[str] = []
    if symbols:
        returned = {bar["symbol"] for bar in bars}
        missing = [symbol for symbol in symbols if symbol not in returned]
        if missing:
            if len(missing) == len(symbols):
                warnings.append("all_symbols_missing")
            else:
                warnings.extend(f"symbol_missing:{symbol}" for symbol in missing)

    result: dict = {"bars": bars}
    if warnings:
        result["warnings"] = warnings
    return result


app = create_agent_app("agent-data", run_data)
