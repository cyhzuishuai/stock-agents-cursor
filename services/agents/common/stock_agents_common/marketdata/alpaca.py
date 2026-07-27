"""Alpaca market-data provider stub (keys wired in a later task)."""

from __future__ import annotations


class AlpacaMarketDataProvider:
    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        raise NotImplementedError("alpaca stub: set keys in later task")
