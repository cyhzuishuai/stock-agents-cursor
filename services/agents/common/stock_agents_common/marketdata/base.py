"""Market data provider protocol."""

from __future__ import annotations

from typing import Protocol


class MarketDataProvider(Protocol):
    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        """Return daily OHLCV bars matching data_result bar fields.

        Missing symbols are omitted; callers should add warnings.
        """
        ...
