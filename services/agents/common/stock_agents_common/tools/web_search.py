"""Web search tool (Tavily by default)."""

from __future__ import annotations

import os
from typing import TYPE_CHECKING, Any

import httpx

if TYPE_CHECKING:
    from stock_agents_common.tools import RunContext

TAVILY_URL = "https://api.tavily.com/search"
_DISABLED_VALUES = {"false", "0", "no"}


def web_search_enabled() -> bool:
    """WEB_SEARCH_ENABLED defaults true; only false/0/no disables."""
    raw = os.environ.get("WEB_SEARCH_ENABLED")
    if raw is None or raw.strip() == "":
        return True
    return raw.strip().lower() not in _DISABLED_VALUES


def _client(ctx: RunContext) -> httpx.Client:
    if ctx.http_client is not None:
        return ctx.http_client
    return httpx.Client(timeout=30.0)


def web_search(
    ctx: RunContext,
    *,
    query: str,
    limit: int = 5,
    **_args: Any,
) -> dict:
    if not web_search_enabled():
        return {"ok": False, "error": "web_search_disabled"}

    api_key = os.environ.get("WEB_SEARCH_API_KEY", "").strip()
    if not api_key:
        return {"ok": False, "error": "missing_web_search_api_key"}

    if not query:
        return {"ok": False, "error": "missing_query"}

    provider = (os.environ.get("WEB_SEARCH_PROVIDER") or "tavily").strip().lower()
    if provider != "tavily":
        return {"ok": False, "error": f"unsupported_web_search_provider:{provider}"}

    owns_client = ctx.http_client is None
    client = _client(ctx)
    try:
        resp = client.post(
            TAVILY_URL,
            json={
                "api_key": api_key,
                "query": query,
                "max_results": int(limit),
            },
        )
        resp.raise_for_status()
        payload = resp.json() or {}
        raw_results = payload.get("results") or []
        results = []
        for row in raw_results[: int(limit)]:
            results.append(
                {
                    "title": row.get("title") or "",
                    "url": row.get("url") or "",
                    "content": row.get("content") or row.get("snippet") or "",
                }
            )
        return {"ok": True, "data": {"results": results, "query": query}}
    except Exception as exc:  # noqa: BLE001
        return {"ok": False, "error": str(exc)}
    finally:
        if owns_client:
            client.close()
