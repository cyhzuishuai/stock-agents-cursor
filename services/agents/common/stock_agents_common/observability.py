"""Optional LangSmith tracing helpers (default off, fail-open)."""

from __future__ import annotations

import logging
import os
from collections.abc import Callable
from typing import TypeVar

logger = logging.getLogger(__name__)

T = TypeVar("T")

_TRUTHY = frozenset({"true", "1", "yes"})


def tracing_enabled() -> bool:
    """True iff LANGSMITH_TRACING is truthy and LANGSMITH_API_KEY is non-empty."""
    flag = (os.environ.get("LANGSMITH_TRACING") or "").strip().lower()
    if flag not in _TRUTHY:
        return False
    return bool((os.environ.get("LANGSMITH_API_KEY") or "").strip())


def _enter_tracing(name: str, fn: Callable[[], T], metadata: dict | None) -> Callable[[], T]:
    """Build a callable that runs ``fn`` under a LangSmith root span.

    Raises on import/setup failure. Does not invoke ``fn``.
    """
    from langsmith import traceable
    from langsmith.run_helpers import tracing_context

    project = (os.environ.get("LANGSMITH_PROJECT") or "").strip() or None
    ctx_kwargs: dict = {"enabled": True}
    if project:
        ctx_kwargs["project_name"] = project
    if metadata:
        ctx_kwargs["metadata"] = metadata

    wrapped = traceable(name=name, metadata=metadata or None)(fn)

    def _runner() -> T:
        with tracing_context(**ctx_kwargs):
            return wrapped()

    return _runner


def run_with_tracing(
    name: str,
    fn: Callable[[], T],
    metadata: dict | None = None,
) -> T:
    """Run ``fn`` under LangSmith when enabled; otherwise call ``fn`` directly.

    Tracing setup/import failures are logged and ``fn`` still runs (fail-open).
    Exceptions raised by ``fn`` itself are not swallowed.

    Note: this does not invent ``langsmith_run_url`` — view runs in the
    configured LangSmith project when tracing is enabled.
    """
    if not tracing_enabled():
        return fn()
    try:
        runner = _enter_tracing(name, fn, metadata)
    except Exception:  # noqa: BLE001 — fail-open: never block trading path
        logger.warning(
            "LangSmith tracing setup failed; continuing without tracing",
            exc_info=True,
        )
        return fn()
    return runner()
