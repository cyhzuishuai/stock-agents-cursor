"""Account and risk context tools (injected request only; no Trading API)."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from stock_agents_common.tools import RunContext


def get_account_view(ctx: RunContext, **_args: Any) -> dict:
    snapshot = ctx.req.get("account_snapshot")
    if snapshot is None:
        return {"ok": False, "error": "missing_account_snapshot"}
    return {"ok": True, "data": snapshot}


def get_risk_context(ctx: RunContext, **_args: Any) -> dict:
    risk = ctx.req.get("risk_context")
    if risk is None:
        return {"ok": True, "data": {}}
    return {"ok": True, "data": risk}
