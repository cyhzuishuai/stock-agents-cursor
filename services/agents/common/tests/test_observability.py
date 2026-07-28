"""Tests for optional LangSmith observability helpers."""

from __future__ import annotations

import logging

import pytest


def test_tracing_disabled_by_default(monkeypatch):
    monkeypatch.delenv("LANGSMITH_TRACING", raising=False)
    monkeypatch.delenv("LANGSMITH_API_KEY", raising=False)
    from stock_agents_common.observability import tracing_enabled

    assert tracing_enabled() is False


def test_run_with_tracing_invokes_fn_when_disabled(monkeypatch):
    monkeypatch.setenv("LANGSMITH_TRACING", "false")
    from stock_agents_common.observability import run_with_tracing

    assert run_with_tracing("t", lambda: 42) == 42


def test_tracing_enabled_requires_flag_and_api_key(monkeypatch):
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    monkeypatch.delenv("LANGSMITH_API_KEY", raising=False)
    from stock_agents_common.observability import tracing_enabled

    assert tracing_enabled() is False

    monkeypatch.setenv("LANGSMITH_API_KEY", "sk-test")
    assert tracing_enabled() is True

    monkeypatch.setenv("LANGSMITH_TRACING", "yes")
    assert tracing_enabled() is True

    monkeypatch.setenv("LANGSMITH_TRACING", "1")
    assert tracing_enabled() is True


def test_run_with_tracing_fail_open_on_setup_error(monkeypatch, caplog):
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    monkeypatch.setenv("LANGSMITH_API_KEY", "sk-test")

    import stock_agents_common.observability as obs

    def _boom(*_args, **_kwargs):
        raise RuntimeError("tracing setup exploded")

    monkeypatch.setattr(obs, "_enter_tracing", _boom)

    with caplog.at_level(logging.WARNING):
        assert obs.run_with_tracing("t", lambda: 7) == 7

    assert any("LangSmith" in r.message or "tracing" in r.message.lower() for r in caplog.records)


def test_run_with_tracing_propagates_fn_errors_when_enabled(monkeypatch):
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    monkeypatch.setenv("LANGSMITH_API_KEY", "sk-test")

    import stock_agents_common.observability as obs

    def _runner_factory(_name, wrapped_fn, _metadata=None):
        def _runner():
            return wrapped_fn()

        return _runner

    monkeypatch.setattr(obs, "_enter_tracing", _runner_factory)

    def _boom():
        raise ValueError("business failure")

    with pytest.raises(ValueError, match="business failure"):
        obs.run_with_tracing("t", _boom)


def test_run_with_tracing_fail_open_on_runner_enter_error(monkeypatch, caplog):
    """Context/runner enter failures must fall back to bare fn() once (not abort)."""
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    monkeypatch.setenv("LANGSMITH_API_KEY", "sk-test")

    import stock_agents_common.observability as obs

    calls = {"n": 0}

    def fn():
        calls["n"] += 1
        return 99

    def _runner_factory(_name, _fn, _metadata=None):
        def _runner():
            raise RuntimeError("tracing context enter failed")

        return _runner

    monkeypatch.setattr(obs, "_enter_tracing", _runner_factory)

    with caplog.at_level(logging.WARNING):
        assert obs.run_with_tracing("t", fn) == 99

    assert calls["n"] == 1
    assert any("LangSmith" in r.message or "tracing" in r.message.lower() for r in caplog.records)


def test_run_with_tracing_no_double_exec_when_fn_raises(monkeypatch):
    """If fn already started and raised, re-raise — do not call bare fn() again."""
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    monkeypatch.setenv("LANGSMITH_API_KEY", "sk-test")

    import stock_agents_common.observability as obs

    calls = {"n": 0}

    def fn():
        calls["n"] += 1
        raise ValueError("business failure")

    def _runner_factory(_name, wrapped_fn, _metadata=None):
        def _runner():
            return wrapped_fn()

        return _runner

    monkeypatch.setattr(obs, "_enter_tracing", _runner_factory)

    with pytest.raises(ValueError, match="business failure"):
        obs.run_with_tracing("t", fn)

    assert calls["n"] == 1
