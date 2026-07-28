"""Tests for free market-data provider (HTTP mocked)."""

from __future__ import annotations

import httpx
import pytest

from stock_agents_common.marketdata.alpaca import AlpacaMarketDataProvider
from stock_agents_common.marketdata.factory import get_provider
from stock_agents_common.marketdata.free import FreeMarketDataProvider


def _yahoo_chart_payload(
    symbol: str,
    *,
    timestamp: int,
    open_: float,
    high: float,
    low: float,
    close: float,
    volume: float,
) -> dict:
    return {
        "chart": {
            "result": [
                {
                    "meta": {"symbol": symbol},
                    "timestamp": [timestamp],
                    "indicators": {
                        "quote": [
                            {
                                "open": [open_],
                                "high": [high],
                                "low": [low],
                                "close": [close],
                                "volume": [volume],
                            }
                        ]
                    },
                }
            ],
            "error": None,
        }
    }


def _yahoo_multi_day_payload(
    symbol: str,
    rows: list[tuple[int, float, float, float, float, float]],
) -> dict:
    timestamps = [r[0] for r in rows]
    return {
        "chart": {
            "result": [
                {
                    "meta": {"symbol": symbol},
                    "timestamp": timestamps,
                    "indicators": {
                        "quote": [
                            {
                                "open": [r[1] for r in rows],
                                "high": [r[2] for r in rows],
                                "low": [r[3] for r in rows],
                                "close": [r[4] for r in rows],
                                "volume": [r[5] for r in rows],
                            }
                        ]
                    },
                }
            ],
            "error": None,
        }
    }


def test_free_lookback_returns_multiple_bars():
    trade_date = "2026-07-22"
    ts1, ts2, ts3 = 1784332800, 1784592000, 1784678400  # Jul 18, 21, 22 UTC

    def handler(request: httpx.Request) -> httpx.Response:
        symbol = request.url.path.rsplit("/", 1)[-1]
        if symbol == "AAPL":
            return httpx.Response(
                200,
                json=_yahoo_multi_day_payload(
                    "AAPL",
                    [
                        (ts1, 188.0, 189.0, 187.0, 188.5, 800_000),
                        (ts2, 189.0, 191.0, 188.5, 190.0, 900_000),
                        (ts3, 190.0, 192.0, 188.0, 191.0, 1_000_000),
                    ],
                ),
            )
        return httpx.Response(404, json={"chart": {"result": None}})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    provider = FreeMarketDataProvider(client=client)
    bars = provider.get_daily_bars(["AAPL"], trade_date, lookback_days=3)

    assert len(bars) == 3
    assert [b["trade_date"] for b in bars] == ["2026-07-18", "2026-07-21", "2026-07-22"]
    assert bars[-1]["close"] == 191.0


def test_free_provider_maps_yahoo_json_to_bars():
    # 2026-07-22 00:00:00 UTC
    trade_date = "2026-07-22"
    ts = 1753142400

    def handler(request: httpx.Request) -> httpx.Response:
        assert "query1.finance.yahoo.com" in request.url.host
        symbol = request.url.path.rsplit("/", 1)[-1]
        if symbol == "AAPL":
            return httpx.Response(
                200,
                json=_yahoo_chart_payload(
                    "AAPL",
                    timestamp=ts,
                    open_=190.0,
                    high=192.0,
                    low=188.0,
                    close=191.0,
                    volume=1_000_000,
                ),
            )
        if symbol == "MSFT":
            return httpx.Response(200, json={"chart": {"result": None, "error": {"code": "Not Found"}}})
        return httpx.Response(404, json={"chart": {"result": None, "error": {"code": "Not Found"}}})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    provider = FreeMarketDataProvider(client=client)

    bars = provider.get_daily_bars(["AAPL", "MSFT"], trade_date)

    assert bars == [
        {
            "symbol": "AAPL",
            "trade_date": trade_date,
            "open": 190.0,
            "high": 192.0,
            "low": 188.0,
            "close": 191.0,
            "volume": 1_000_000.0,
        }
    ]


def test_get_provider_free_and_env_default(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("MARKET_DATA_PROVIDER", raising=False)
    assert isinstance(get_provider("free"), FreeMarketDataProvider)

    monkeypatch.setenv("MARKET_DATA_PROVIDER", "free")
    assert isinstance(get_provider(), FreeMarketDataProvider)


def test_get_provider_alpaca():
    provider = get_provider("alpaca")
    assert isinstance(provider, AlpacaMarketDataProvider)


def test_get_provider_reads_market_data_provider_env(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("MARKET_DATA_PROVIDER", "alpaca")
    assert isinstance(get_provider(), AlpacaMarketDataProvider)
