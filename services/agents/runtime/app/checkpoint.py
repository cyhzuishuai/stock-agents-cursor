"""LangGraph SQLite checkpointer helper."""

from __future__ import annotations

import os
import sqlite3
from pathlib import Path

from langgraph.checkpoint.sqlite import SqliteSaver

_checkpointer = None
_checkpointer_conn = None


class CheckpointUnavailableError(RuntimeError):
    pass


def reset_checkpointer_for_tests() -> None:
    global _checkpointer, _checkpointer_conn
    if _checkpointer_conn is not None:
        try:
            _checkpointer_conn.close()
        except Exception:  # noqa: BLE001 — best-effort test cleanup
            pass
    _checkpointer = None
    _checkpointer_conn = None


def default_thread_id(run_id: str, agent: str) -> str:
    return f"{run_id}:{agent}"


def get_checkpointer():
    """Return a process-wide SqliteSaver.

    ``SqliteSaver.from_conn_string`` is a context manager that closes the DB on
    exit; we open a long-lived ``sqlite3`` connection instead so the singleton
    survives beyond the caller's stack frame.
    """
    global _checkpointer, _checkpointer_conn
    if _checkpointer is not None:
        return _checkpointer
    path = os.environ.get("AGENT_CHECKPOINT_SQLITE_PATH", "/data/checkpoints.sqlite")
    try:
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        conn = sqlite3.connect(path, check_same_thread=False)
        _checkpointer_conn = conn
        _checkpointer = SqliteSaver(conn)
        return _checkpointer
    except Exception as exc:
        raise CheckpointUnavailableError(str(exc)) from exc
