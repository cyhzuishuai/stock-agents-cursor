"""Free market-data provider using Yahoo Finance chart API (HTTP via httpx).

Source: ``https://query1.finance.yahoo.com/v8/finance/chart/{symbol}``
with ``interval=1d`` for the requested trade date window.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

import httpx

_YAHOO_CHART = "https://query1.finance.yahoo.com/v8/finance/chart/{symbol}"


class FreeMarketDataProvider:
    """Fetches daily OHLCV from Yahoo Finance; maps to data_result bar dicts."""

    def __init__(self, client: httpx.Client | None = None) -> None:
        self._client = client
        self._owns_client = client is None

    def _client_or_default(self) -> httpx.Client:
        if self._client is None:
            self._client = httpx.Client(
                timeout=30.0,
                headers={"User-Agent": "stock-agents/0.1"},
            )
        return self._client

    def close(self) -> None:
        if self._owns_client and self._client is not None:
            self._client.close()
            self._client = None

    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        day = datetime.strptime(trade_date, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        period1 = int(day.timestamp())
        period2 = int((day + timedelta(days=1)).timestamp())

        bars: list[dict] = []
        client = self._client_or_default()
        for symbol in symbols:
            bar = self._fetch_symbol(client, symbol, trade_date, period1, period2)
            if bar is not None:
                bars.append(bar)
        return bars

    def _fetch_symbol(
        self,
        client: httpx.Client,
        symbol: str,
        trade_date: str,
        period1: int,
        period2: int,
    ) -> dict | None:
        url = _YAHOO_CHART.format(symbol=symbol)
        try:
            response = client.get(
                url,
                params={"interval": "1d", "period1": period1, "period2": period2},
            )
        except httpx.HTTPError:
            return None

        if response.status_code != 200:
            return None

        try:
            payload = response.json()
        except ValueError:
            return None

        result = (payload.get("chart") or {}).get("result")
        if not result:
            return None

        row = result[0]
        timestamps = row.get("timestamp") or []
        quote = ((row.get("indicators") or {}).get("quote") or [{}])[0]
        if not timestamps:
            return None

        # Prefer the bar whose UTC date matches trade_date; else first bar.
        idx = 0
        for i, ts in enumerate(timestamps):
            if datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%d") == trade_date:
                idx = i
                break

        def _num(key: str) -> float | None:
            values = quote.get(key) or []
            if idx >= len(values) or values[idx] is None:
                return None
            return float(values[idx])

        open_, high, low, close, volume = (
            _num("open"),
            _num("high"),
            _num("low"),
            _num("close"),
            _num("volume"),
        )
        if None in (open_, high, low, close, volume):
            return None

        return {
            "symbol": symbol,
            "trade_date": trade_date,
            "open": open_,
            "high": high,
            "low": low,
            "close": close,
            "volume": volume,
        }
