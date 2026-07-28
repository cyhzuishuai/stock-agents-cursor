"""Tests for LLMClient mock mode."""

from __future__ import annotations

import json

import httpx
import pytest

from stock_agents_common.llm import LLMClient
from stock_agents_common.schemas import validate


@pytest.fixture(autouse=True)
def _clear_llm_mode(monkeypatch):
    monkeypatch.delenv("LLM_MODE", raising=False)


def test_mock_mode_returns_parsed_json(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")

    client = LLMClient()
    result = client.complete_json("system prompt", "user prompt", "research_result")

    assert isinstance(result, dict)
    validate(result, "research_result")
    assert result["items"][0]["bias"] in {"bull", "bear", "neutral"}


def test_mock_mode_supports_multiple_schemas(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")

    client = LLMClient()
    decision = client.complete_json("sys", "user", "decision_result")

    validate(decision, "decision_result")
    assert decision["intents"][0]["side"] in {"buy", "sell", "hold"}


def test_real_mode_calls_openai_compatible_api(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://llm.example/v1")
    monkeypatch.setenv("LLM_MODEL", "gpt-4o-mini")

    payload = {
        "intents": [
            {
                "symbol": "AAPL",
                "side": "buy",
                "urgency": "normal",
                "rationale": "From LLM.",
            }
        ],
        "warnings": [],
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url == "https://llm.example/v1/chat/completions"
        assert request.headers["authorization"] == "Bearer test-key"
        body = json.loads(request.content)
        assert body["model"] == "gpt-4o-mini"
        assert body["response_format"] == {"type": "json_object"}
        return httpx.Response(
            200,
            json={"choices": [{"message": {"content": json.dumps(payload)}}]},
        )

    client = LLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_json("sys", "user", "decision_result")

    validate(result, "decision_result")
    assert result == payload


def test_real_mode_requires_api_key(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")

    client = LLMClient()
    with pytest.raises(ValueError, match="LLM_API_KEY or LLM_PRIMARY_API_KEY"):
        client.complete_json("sys", "user", "research_result")


def test_real_mode_uses_primary_env(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")

    payload = {
        "intents": [
            {"symbol": "AAPL", "side": "buy", "urgency": "normal", "rationale": "From LLM."}
        ],
        "warnings": [],
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == "https://ark.example/api/v3/chat/completions"
        body = json.loads(request.content)
        assert body["model"] == "Doubao-Smart-Router"
        return httpx.Response(
            200,
            json={"choices": [{"message": {"content": json.dumps(payload)}}]},
        )

    client = LLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_json("sys", "user", "decision_result")
    assert result == payload


def test_real_mode_empty_llm_model_falls_back_to_default(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_MODEL", "")

    payload = {
        "intents": [
            {
                "symbol": "AAPL",
                "side": "hold",
                "urgency": "normal",
                "rationale": "From LLM.",
            }
        ],
        "warnings": [],
    }

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        assert body["model"] == "gpt-4o-mini"
        return httpx.Response(
            200,
            json={"choices": [{"message": {"content": json.dumps(payload)}}]},
        )

    client = LLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_json("sys", "user", "decision_result")

    validate(result, "decision_result")
    assert result == payload
