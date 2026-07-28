from __future__ import annotations

import httpx
import pytest

from stock_agents_common.tools import RunContext, web_search


def _ctx(*, http_client: httpx.Client | None = None) -> RunContext:
    return RunContext(
        req={
            "trade_date": "2026-07-22",
            "watchlist": ["AAPL"],
            "account_snapshot": {"cash": 10000.0, "currency": "USD", "positions": []},
        },
        http_client=http_client,
    )


def test_web_search_tavily_success(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("WEB_SEARCH_ENABLED", "true")
    monkeypatch.setenv("WEB_SEARCH_API_KEY", "tvly-test")
    monkeypatch.setenv("WEB_SEARCH_PROVIDER", "tavily")

    def handler(request: httpx.Request) -> httpx.Response:
        assert "api.tavily.com/search" in str(request.url)
        body = request.read()
        assert b"tvly-test" in body
        assert b"AAPL earnings" in body
        return httpx.Response(
            200,
            json={
                "results": [
                    {
                        "title": "AAPL beats",
                        "url": "https://example.com/aapl",
                        "content": "Apple reported...",
                    }
                ]
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = web_search(_ctx(http_client=client), query="AAPL earnings", limit=5)

    assert result["ok"] is True
    assert len(result["data"]["results"]) == 1
    assert result["data"]["results"][0]["title"] == "AAPL beats"


def test_web_search_missing_key_returns_ok_false(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("WEB_SEARCH_ENABLED", "true")
    monkeypatch.delenv("WEB_SEARCH_API_KEY", raising=False)

    result = web_search(_ctx(), query="AAPL")

    assert result == {"ok": False, "error": "missing_web_search_api_key"}


@pytest.mark.parametrize("value", ["false", "0", "no", "FALSE", "No"])
def test_web_search_disabled_returns_ok_false(monkeypatch: pytest.MonkeyPatch, value: str):
    monkeypatch.setenv("WEB_SEARCH_ENABLED", value)
    monkeypatch.setenv("WEB_SEARCH_API_KEY", "tvly-test")

    result = web_search(_ctx(), query="AAPL")

    assert result["ok"] is False
    assert result["error"] == "web_search_disabled"


@pytest.mark.parametrize("value", ["", "true", "1", "yes", "TRUE"])
def test_web_search_enabled_by_default_values(monkeypatch: pytest.MonkeyPatch, value: str):
    if value == "":
        monkeypatch.delenv("WEB_SEARCH_ENABLED", raising=False)
    else:
        monkeypatch.setenv("WEB_SEARCH_ENABLED", value)
    monkeypatch.delenv("WEB_SEARCH_API_KEY", raising=False)

    # Enabled path still needs a key; missing key proves we did not short-circuit as disabled.
    result = web_search(_ctx(), query="AAPL")
    assert result == {"ok": False, "error": "missing_web_search_api_key"}


def test_web_search_upstream_error_degrades(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("WEB_SEARCH_ENABLED", "true")
    monkeypatch.setenv("WEB_SEARCH_API_KEY", "tvly-test")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, text="unavailable")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = web_search(_ctx(http_client=client), query="AAPL")

    assert result["ok"] is False
    assert "error" in result
