"""Market data provider protocol."""

from __future__ import annotations

from typing import Protocol


class MarketDataProvider(Protocol):
    def get_daily_bars(
        self,
        symbols: list[str],
        trade_date: str,
        *,
        lookback_days: int = 1,
    ) -> list[dict]:
        """Return daily OHLCV bars matching data_result bar fields.

        When lookback_days > 1, returns up to lookback_days sessions per symbol
        ending on trade_date. Missing symbols are omitted; callers should add warnings.
        """
        ...
