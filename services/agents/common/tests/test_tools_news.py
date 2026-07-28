from __future__ import annotations

import httpx
import pytest

from stock_agents_common.tools import RunContext, get_news


def _ctx(*, http_client: httpx.Client | None = None) -> RunContext:
    return RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL"],
            "account_snapshot": {"cash": 10000.0, "currency": "USD", "positions": []},
        },
        http_client=http_client,
    )


def test_get_news_returns_top_3_from_finnhub(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("FINNHUB_API_KEY", "fh-test")

    payload = [
        {
            "headline": f"Story {i}",
            "summary": f"Summary {i}",
            "datetime": 1_700_000_000 + i,
            "source": "Reuters",
            "url": f"https://example.com/{i}",
        }
        for i in range(5)
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        assert "finnhub.io/api/v1/company-news" in str(request.url)
        assert request.url.params.get("symbol") == "AAPL"
        assert request.url.params.get("token") == "fh-test"
        assert request.url.params.get("from")
        assert request.url.params.get("to")
        return httpx.Response(200, json=payload)

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = get_news(_ctx(http_client=client), symbol="AAPL")

    assert result["ok"] is True
    assert len(result["data"]["items"]) == 3
    assert result["data"]["items"][0]["headline"] == "Story 0"
    assert set(result["data"]["items"][0]) >= {
        "headline",
        "summary",
        "datetime",
        "source",
        "url",
    }


def test_get_news_missing_key_returns_ok_false(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("FINNHUB_API_KEY", raising=False)

    result = get_news(_ctx(), symbol="AAPL")

    assert result == {"ok": False, "error": "missing_finnhub_api_key"}


def test_get_news_upstream_error_degrades(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("FINNHUB_API_KEY", "fh-test")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, text="boom")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = get_news(_ctx(http_client=client), symbol="AAPL")

    assert result["ok"] is False
    assert "error" in result
