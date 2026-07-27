from __future__ import annotations

import httpx
import pytest

from stock_agents_common.marketdata.alpaca import AlpacaMarketDataProvider


def test_get_daily_bars_maps_alpaca_bars(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ALPACA_API_KEY", "k")
    monkeypatch.setenv("ALPACA_API_SECRET", "s")

    payload = {
        "bars": {
            "AAPL": [
                {
                    "t": "2026-07-22T04:00:00Z",
                    "o": 100.0,
                    "h": 110.0,
                    "l": 99.0,
                    "c": 105.0,
                    "v": 1_000_000,
                }
            ]
        }
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers.get("APCA-API-KEY-ID") == "k"
        assert request.headers.get("APCA-API-SECRET-KEY") == "s"
        assert "/v2/stocks/bars" in str(request.url)
        return httpx.Response(200, json=payload)

    transport = httpx.MockTransport(handler)
    client = httpx.Client(transport=transport)
    provider = AlpacaMarketDataProvider(client=client)
    bars = provider.get_daily_bars(["AAPL"], "2026-07-22")
    assert bars == [
        {
            "symbol": "AAPL",
            "trade_date": "2026-07-22",
            "open": 100.0,
            "high": 110.0,
            "low": 99.0,
            "close": 105.0,
            "volume": 1_000_000.0,
        }
    ]


def test_get_daily_bars_requires_keys(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("ALPACA_API_KEY", raising=False)
    monkeypatch.delenv("ALPACA_API_SECRET", raising=False)
    provider = AlpacaMarketDataProvider(client=httpx.Client(transport=httpx.MockTransport(lambda r: httpx.Response(500))))
    with pytest.raises(ValueError, match="ALPACA_API_KEY"):
        provider.get_daily_bars(["AAPL"], "2026-07-22")
