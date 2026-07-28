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

    def _runner_factory(*_args, **_kwargs):
        def _runner():
            raise ValueError("business failure")

        return _runner

    monkeypatch.setattr(obs, "_enter_tracing", _runner_factory)

    with pytest.raises(ValueError, match="business failure"):
        obs.run_with_tracing("t", lambda: 1)
