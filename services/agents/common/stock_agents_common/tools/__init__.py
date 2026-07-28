"""Agent tool implementations and shared RunContext."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import httpx

from stock_agents_common.tools.account import get_account_view, get_risk_context
from stock_agents_common.tools.bars import get_daily_bars, get_last_closes
from stock_agents_common.tools.news import get_news
from stock_agents_common.tools.sizing import size_proposals
from stock_agents_common.tools.web_search import web_search


@dataclass
class RunContext:
    """Per-run tool context: injected request plus optional HTTP / marketdata deps."""

    req: dict[str, Any]
    http_client: httpx.Client | None = None
    marketdata_provider: Any | None = None
    extra: dict[str, Any] = field(default_factory=dict)


__all__ = [
    "RunContext",
    "get_account_view",
    "get_daily_bars",
    "get_last_closes",
    "get_news",
    "get_risk_context",
    "size_proposals",
    "web_search",
]
