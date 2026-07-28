"""Finnhub company-news tool."""

from __future__ import annotations

import os
from datetime import datetime, timedelta, timezone
from typing import TYPE_CHECKING, Any

import httpx

if TYPE_CHECKING:
    from stock_agents_common.tools import RunContext

FINNHUB_BASE = "https://finnhub.io/api/v1"
TOP_N = 3


def _client(ctx: RunContext) -> httpx.Client:
    if ctx.http_client is not None:
        return ctx.http_client
    return httpx.Client(timeout=30.0)


def _default_from_to(trade_date: str | None) -> tuple[str, str]:
    if trade_date:
        try:
            end = datetime.strptime(trade_date, "%Y-%m-%d").replace(tzinfo=timezone.utc)
        except ValueError:
            end = datetime.now(timezone.utc)
    else:
        end = datetime.now(timezone.utc)
    start = end - timedelta(days=7)
    return start.strftime("%Y-%m-%d"), end.strftime("%Y-%m-%d")


def get_news(
    ctx: RunContext,
    *,
    symbol: str,
    from_date: str | None = None,
    to_date: str | None = None,
    **_args: Any,
) -> dict:
    api_key = os.environ.get("FINNHUB_API_KEY", "").strip()
    if not api_key:
        return {"ok": False, "error": "missing_finnhub_api_key"}

    if not symbol:
        return {"ok": False, "error": "missing_symbol"}

    default_from, default_to = _default_from_to(ctx.req.get("trade_date"))
    params = {
        "symbol": symbol,
        "from": from_date or default_from,
        "to": to_date or default_to,
        "token": api_key,
    }

    owns_client = ctx.http_client is None
    client = _client(ctx)
    try:
        resp = client.get(f"{FINNHUB_BASE}/company-news", params=params)
        resp.raise_for_status()
        raw = resp.json()
        if not isinstance(raw, list):
            return {"ok": False, "error": "invalid_finnhub_response"}
        items = []
        for row in raw[:TOP_N]:
            items.append(
                {
                    "headline": row.get("headline") or "",
                    "summary": row.get("summary") or "",
                    "datetime": row.get("datetime"),
                    "source": row.get("source") or "",
                    "url": row.get("url") or "",
                }
            )
        return {"ok": True, "data": {"items": items, "symbol": symbol}}
    except Exception as exc:  # noqa: BLE001
        return {"ok": False, "error": str(exc)}
    finally:
        if owns_client:
            client.close()
