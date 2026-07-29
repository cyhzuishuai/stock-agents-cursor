"""LangGraph SQLite checkpointer helper."""

from __future__ import annotations

import os
from pathlib import Path

from langgraph.checkpoint.sqlite import SqliteSaver

_checkpointer = None


class CheckpointUnavailableError(RuntimeError):
    pass


def reset_checkpointer_for_tests() -> None:
    global _checkpointer
    _checkpointer = None


def default_thread_id(run_id: str, agent: str) -> str:
    return f"{run_id}:{agent}"


def get_checkpointer():
    global _checkpointer
    if _checkpointer is not None:
        return _checkpointer
    path = os.environ.get("AGENT_CHECKPOINT_SQLITE_PATH", "/data/checkpoints.sqlite")
    try:
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        # SqliteSaver.from_conn_string may be a context manager in some versions —
        # keep a long-lived connection; follow installed package docs.
        _checkpointer = SqliteSaver.from_conn_string(path)
        if hasattr(_checkpointer, "__enter__"):
            _checkpointer = _checkpointer.__enter__()
        return _checkpointer
    except Exception as exc:
        raise CheckpointUnavailableError(str(exc)) from exc
