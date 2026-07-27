"""Alpaca Market Data daily bars provider."""

from __future__ import annotations

import os
from datetime import datetime, timedelta, timezone

import httpx


class AlpacaMarketDataProvider:
    def __init__(
        self,
        client: httpx.Client | None = None,
        *,
        api_key: str | None = None,
        api_secret: str | None = None,
        base_url: str | None = None,
    ) -> None:
        self._client = client
        self._owns_client = client is None
        self._api_key = api_key if api_key is not None else os.getenv("ALPACA_API_KEY", "")
        self._api_secret = api_secret if api_secret is not None else os.getenv("ALPACA_API_SECRET", "")
        self._base_url = (
            base_url
            or os.getenv("ALPACA_DATA_BASE_URL")
            or "https://data.alpaca.markets"
        ).rstrip("/")

    def close(self) -> None:
        if self._owns_client and self._client is not None:
            self._client.close()
            self._client = None

    def _client_or_default(self) -> httpx.Client:
        if self._client is None:
            self._client = httpx.Client(timeout=30.0)
        return self._client

    def get_daily_bars(self, symbols: list[str], trade_date: str) -> list[dict]:
        if not self._api_key or not self._api_secret:
            raise ValueError("ALPACA_API_KEY and ALPACA_API_SECRET are required")
        day = datetime.strptime(trade_date, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        start = day.isoformat().replace("+00:00", "Z")
        end = (day + timedelta(days=1)).isoformat().replace("+00:00", "Z")
        client = self._client_or_default()
        resp = client.get(
            f"{self._base_url}/v2/stocks/bars",
            params={
                "symbols": ",".join(symbols),
                "timeframe": "1Day",
                "start": start,
                "end": end,
                "adjustment": "raw",
                "feed": "iex",
            },
            headers={
                "APCA-API-KEY-ID": self._api_key,
                "APCA-API-SECRET-KEY": self._api_secret,
            },
        )
        resp.raise_for_status()
        raw_bars = (resp.json() or {}).get("bars") or {}
        out: list[dict] = []
        for symbol in symbols:
            rows = raw_bars.get(symbol) or []
            if not rows:
                continue
            row = rows[0]
            out.append(
                {
                    "symbol": symbol,
                    "trade_date": trade_date,
                    "open": float(row["o"]),
                    "high": float(row["h"]),
                    "low": float(row["l"]),
                    "close": float(row["c"]),
                    "volume": float(row["v"]),
                }
            )
        return out
