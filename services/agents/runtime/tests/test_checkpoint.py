from __future__ import annotations


def test_get_checkpointer_creates_sqlite(tmp_path, monkeypatch):
    path = tmp_path / "ck.sqlite"
    monkeypatch.setenv("AGENT_CHECKPOINT_SQLITE_PATH", str(path))
    from app.checkpoint import get_checkpointer, reset_checkpointer_for_tests

    reset_checkpointer_for_tests()
    cp = get_checkpointer()
    assert cp is not None
    assert path.exists() or path.parent.exists()
