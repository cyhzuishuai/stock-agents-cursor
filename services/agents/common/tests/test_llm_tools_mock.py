"""Tests for ToolLLMClient mock and real tool-calling modes."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest

from stock_agents_common.llm_tools import ToolLLMClient
from stock_agents_common.schemas import validate


REPO_ROOT = Path(__file__).resolve().parents[4]
ANALYST_SCRIPT = REPO_ROOT / "packages" / "contracts" / "fixtures" / "mock_tool_scripts" / "analyst.json"


@pytest.fixture(autouse=True)
def _clear_llm_env(monkeypatch):
    monkeypatch.delenv("LLM_MODE", raising=False)
    monkeypatch.delenv("MOCK_TOOL_SCRIPT", raising=False)


def test_mock_mode_returns_tool_calls_then_final_json(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(ANALYST_SCRIPT))

    client = ToolLLMClient()
    tools = [
        {
            "type": "function",
            "function": {
                "name": "get_account_view",
                "description": "Account snapshot",
                "parameters": {"type": "object", "properties": {}},
            },
        }
    ]

    round1 = client.complete_tools("system", [{"role": "user", "content": "analyze"}], tools)
    assert round1["tool_calls"] is not None
    assert len(round1["tool_calls"]) >= 1
    assert round1["tool_calls"][0]["name"] == "get_account_view"
    assert "id" in round1["tool_calls"][0]
    assert isinstance(round1["tool_calls"][0]["args"], dict)
    assert round1["content"] is None or round1["content"] == ""
    assert "usage" in round1
    assert "latency_ms" in round1
    assert isinstance(round1["latency_ms"], int)

    round2 = client.complete_tools(
        "system",
        [
            {"role": "user", "content": "analyze"},
            {"role": "assistant", "tool_calls": round1["tool_calls"]},
            {"role": "tool", "tool_call_id": round1["tool_calls"][0]["id"], "content": "{}"},
        ],
        tools,
    )
    assert round2["tool_calls"][0]["name"] == "get_daily_bars"

    final = client.complete_tools("system", [{"role": "user", "content": "analyze"}], tools)
    assert final["tool_calls"] is None or final["tool_calls"] == []
    assert final["content"]
    payload = json.loads(final["content"]) if isinstance(final["content"], str) else final["content"]
    validate(payload, "analyst_result")
    assert payload["items"][0]["symbol"] == "AAPL"


def test_mock_mode_uses_default_analyst_fixture(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "mock")
    # No MOCK_TOOL_SCRIPT → default packages/contracts/fixtures/mock_tool_scripts/analyst.json

    client = ToolLLMClient()
    first = client.complete_tools("sys", [{"role": "user", "content": "go"}], [])
    assert first["tool_calls"]
    assert first["tool_calls"][0]["name"] in {
        "get_account_view",
        "get_daily_bars",
        "get_news",
        "web_search",
        "get_risk_context",
    }


def test_mock_mode_round_index_advances_per_client_call(monkeypatch, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = {
        "rounds": [
            {"tool_calls": [{"id": "t1", "name": "get_account_view", "args": {}}]},
            {"tool_calls": [{"id": "t2", "name": "get_daily_bars", "args": {"lookback_days": 20}}]},
            {
                "content_json": {
                    "items": [
                        {
                            "symbol": "AAPL",
                            "bias": "bull",
                            "confidence": 0.7,
                            "thesis": "Up",
                            "side": "buy",
                            "urgency": "normal",
                            "rationale": "Bars",
                        }
                    ],
                    "warnings": [],
                }
            },
        ]
    }
    path = tmp_path / "script.json"
    path.write_text(json.dumps(script), encoding="utf-8")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(path))

    client = ToolLLMClient()
    r0 = client.complete_tools("s", [{"role": "user", "content": "x"}], [])
    r1 = client.complete_tools("s", [{"role": "user", "content": "x"}], [])
    r2 = client.complete_tools("s", [{"role": "user", "content": "x"}], [])

    assert r0["tool_calls"][0]["name"] == "get_account_view"
    assert r1["tool_calls"][0]["name"] == "get_daily_bars"
    assert r2["tool_calls"] in (None, [])
    content = json.loads(r2["content"])
    assert content["items"][0]["side"] == "buy"

    # Exhausted rounds should raise
    with pytest.raises(IndexError):
        client.complete_tools("s", [{"role": "user", "content": "x"}], [])


def test_real_mode_parses_tool_calls(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://llm.example/v1")
    monkeypatch.setenv("LLM_MODEL", "gpt-4o-mini")

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url == "https://llm.example/v1/chat/completions"
        assert request.headers["authorization"] == "Bearer test-key"
        body = json.loads(request.content)
        assert body["model"] == "gpt-4o-mini"
        assert "tools" in body
        assert body["messages"][0]["role"] == "system"
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": None,
                            "tool_calls": [
                                {
                                    "id": "call_1",
                                    "type": "function",
                                    "function": {
                                        "name": "get_account_view",
                                        "arguments": "{}",
                                    },
                                }
                            ],
                        }
                    }
                ],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_tools(
        "system",
        [{"role": "user", "content": "run"}],
        [{"type": "function", "function": {"name": "get_account_view", "parameters": {"type": "object"}}}],
    )

    assert result["tool_calls"][0]["id"] == "call_1"
    assert result["tool_calls"][0]["name"] == "get_account_view"
    assert result["tool_calls"][0]["args"] == {}
    assert result["usage"]["total_tokens"] == 15
    assert isinstance(result["latency_ms"], int)


def test_real_mode_final_json_content(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://llm.example/v1")

    payload = {
        "items": [
            {
                "symbol": "AAPL",
                "bias": "bull",
                "confidence": 0.8,
                "thesis": "Strong",
                "side": "buy",
                "urgency": "normal",
                "rationale": "News",
            }
        ],
        "warnings": [],
    }

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        # Finalize: empty tools → may request json_object
        assert body.get("tools") in (None, [])
        assert body.get("response_format") == {"type": "json_object"}
        return httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": json.dumps(payload), "tool_calls": None}}],
                "usage": {"total_tokens": 20},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    result = client.complete_tools("system", [{"role": "user", "content": "finalize"}], tools_openai_schema=[])

    assert result["tool_calls"] in (None, [])
    assert json.loads(result["content"]) == payload


def test_real_mode_requires_api_key(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")

    client = ToolLLMClient()
    with pytest.raises(ValueError, match="LLM_API_KEY"):
        client.complete_tools("sys", [{"role": "user", "content": "x"}], [])
