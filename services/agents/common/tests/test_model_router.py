"""Tests for ModelRouter env resolution and failover."""

from __future__ import annotations

import json

import httpx
import pytest

from stock_agents_common.model_router import chat_completions, resolve_providers


@pytest.fixture(autouse=True)
def _clear_llm_env(monkeypatch):
    for key in (
        "LLM_API_KEY",
        "LLM_BASE_URL",
        "LLM_MODEL",
        "LLM_PRIMARY_API_KEY",
        "LLM_PRIMARY_BASE_URL",
        "LLM_PRIMARY_MODEL",
        "LLM_FALLBACK_API_KEY",
        "LLM_FALLBACK_BASE_URL",
        "LLM_FALLBACK_MODEL",
    ):
        monkeypatch.delenv(key, raising=False)


def test_resolve_legacy_single_provider(monkeypatch):
    monkeypatch.setenv("LLM_API_KEY", "legacy-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.minimaxi.com/v1")
    monkeypatch.setenv("LLM_MODEL", "MiniMax-M3")
    primary, fallback = resolve_providers()
    assert primary.api_key == "legacy-key"
    assert primary.base_url == "https://api.minimaxi.com/v1"
    assert primary.model == "MiniMax-M3"
    assert fallback is None


def test_resolve_primary_and_fallback(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://api.minimaxi.com/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")
    primary, fallback = resolve_providers()
    assert primary.api_key == "ark-key"
    assert primary.base_url == "https://ark.cn-beijing.volces.com/api/v3"
    assert primary.model == "Doubao-Smart-Router"
    assert fallback is not None
    assert fallback.api_key == "mm-key"
    assert fallback.model == "MiniMax-M3"


def test_chat_completions_uses_primary(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://ark.example/api/v3/chat/completions"
        assert request.headers["authorization"] == "Bearer ark-key"
        body = json.loads(request.content)
        assert body["model"] == "Doubao-Smart-Router"
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is False
    assert result.provider == "primary"
    assert result.model == "Doubao-Smart-Router"
    assert result.response.status_code == 200


def test_chat_completions_falls_back_on_http_500(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    calls: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(str(request.url))
        if "ark.example" in str(request.url):
            return httpx.Response(500, text="upstream down")
        body = json.loads(request.content)
        assert body["model"] == "MiniMax-M3"
        assert request.headers["authorization"] == "Bearer mm-key"
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is True
    assert result.provider == "fallback"
    assert result.model == "MiniMax-M3"
    assert result.primary_error and "500" in result.primary_error
    assert len(calls) == 2


def test_chat_completions_falls_back_on_connect_error(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    def handler(request: httpx.Request) -> httpx.Response:
        if "ark.example" in str(request.url):
            raise httpx.ConnectError("boom", request=request)
        return httpx.Response(200, json={"choices": [{"message": {"content": "{}"}}]})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    result = chat_completions(payload={"messages": []}, http_client=client)
    assert result.fallback_used is True
    assert result.provider == "fallback"


def test_chat_completions_both_fail_raises(monkeypatch):
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, text="nope")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    with pytest.raises(ValueError, match="fallback failed"):
        chat_completions(payload={"messages": []}, http_client=client)
