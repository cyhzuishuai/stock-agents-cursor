"""Factory for market-data providers (``MARKET_DATA_PROVIDER``)."""

from __future__ import annotations

import os

from stock_agents_common.marketdata.alpaca import AlpacaMarketDataProvider
from stock_agents_common.marketdata.base import MarketDataProvider
from stock_agents_common.marketdata.free import FreeMarketDataProvider


def get_provider(name: str | None = None) -> MarketDataProvider:
    """Return a provider by name, or ``MARKET_DATA_PROVIDER`` (default ``free``)."""
    resolved = (name if name is not None else os.environ.get("MARKET_DATA_PROVIDER", "free")).strip().lower()
    if resolved == "free":
        return FreeMarketDataProvider()
    if resolved == "alpaca":
        return AlpacaMarketDataProvider()
    raise ValueError(f"unknown market data provider: {resolved!r}")
