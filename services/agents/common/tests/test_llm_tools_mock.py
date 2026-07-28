"""Tests for ToolLLMClient mock and real tool-calling modes."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest

from stock_agents_common.llm_tools import ToolLLMClient, extract_json_from_content
from stock_agents_common.schemas import validate


REPO_ROOT = Path(__file__).resolve().parents[4]
ANALYST_SCRIPT = REPO_ROOT / "packages" / "contracts" / "fixtures" / "mock_tool_scripts" / "analyst.json"


@pytest.fixture(autouse=True)
def _clear_llm_env(monkeypatch):
    monkeypatch.delenv("LLM_MODE", raising=False)
    monkeypatch.delenv("MOCK_TOOL_SCRIPT", raising=False)


def test_mock_complete_plan_and_reflect_do_not_consume_tool_rounds(monkeypatch, tmp_path):
    monkeypatch.setenv("LLM_MODE", "mock")
    script = {
        "plan": {"steps": [{"id": "s1", "title": "Bars", "status": "pending"}]},
        "reflect": [
            {"decision": "mark_step_done", "step_id": "s1", "reason": "ok"},
            {"decision": "finalize", "reason": "done"},
        ],
        "rounds": [{"tool_calls": [{"id": "1", "name": "get_news", "args": {"symbol": "AAPL"}}]}],
    }
    path = tmp_path / "script.json"
    path.write_text(json.dumps(script), encoding="utf-8")
    monkeypatch.setenv("MOCK_TOOL_SCRIPT", str(path))

    client = ToolLLMClient()
    plan = client.complete_plan("sys", "user")
    assert plan["plan_steps"][0]["id"] == "s1"
    tools = client.complete_tools("sys", [{"role": "user", "content": "x"}], [])
    assert tools["tool_calls"][0]["name"] == "get_news"
    r1 = client.complete_reflect("sys", [])
    assert r1["reflect"]["decision"] == "mark_step_done"
    r2 = client.complete_reflect("sys", [])
    assert r2["reflect"]["decision"] == "finalize"


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


def test_live_tool_client_falls_back_on_primary_http_error(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_PRIMARY_API_KEY", "ark-key")
    monkeypatch.setenv("LLM_PRIMARY_BASE_URL", "https://ark.example/api/v3")
    monkeypatch.setenv("LLM_PRIMARY_MODEL", "Doubao-Smart-Router")
    monkeypatch.setenv("LLM_FALLBACK_API_KEY", "mm-key")
    monkeypatch.setenv("LLM_FALLBACK_BASE_URL", "https://mm.example/v1")
    monkeypatch.setenv("LLM_FALLBACK_MODEL", "MiniMax-M3")

    def handler(request: httpx.Request) -> httpx.Response:
        if "ark.example" in str(request.url):
            return httpx.Response(500, text="down")
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": None,
                            "tool_calls": [
                                {
                                    "id": "1",
                                    "type": "function",
                                    "function": {"name": "get_news", "arguments": "{\"symbol\":\"AAPL\"}"},
                                }
                            ],
                        }
                    }
                ],
                "usage": {"prompt_tokens": 1, "completion_tokens": 1},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    out = client.complete_tools("sys", [{"role": "user", "content": "hi"}], [])
    assert out["router"]["fallback_used"] is True
    assert out["router"]["model"] == "MiniMax-M3"
    assert out["tool_calls"]


def test_extract_json_strips_think_tags_and_markdown_fence():
    payload = {
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
    raw = (
        "<think>reasoning about the market...</think>\n"
        "```json\n"
        f"{json.dumps(payload)}\n"
        "```\n"
    )
    assert extract_json_from_content(raw) == payload


def test_extract_json_handles_plain_json_and_invalid():
    assert extract_json_from_content('{"a": 1}') == {"a": 1}
    assert extract_json_from_content(None) is None
    assert extract_json_from_content("") is None
    assert extract_json_from_content("not json at all") is None
    assert extract_json_from_content("<think>only thoughts</think>") is None


def test_minimax_payload_disables_thinking_by_default(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.minimax.chat/v1")
    monkeypatch.setenv("LLM_MODEL", "MiniMax-M3")
    monkeypatch.delenv("LLM_THINKING", raising=False)
    monkeypatch.delenv("LLM_REASONING_SPLIT", raising=False)

    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        return httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": '{"ok": true}', "tool_calls": None}}],
                "usage": {},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    client.complete_tools("system", [{"role": "user", "content": "x"}], [])

    assert captured["thinking"] == {"type": "disabled"}
    assert "reasoning_split" not in captured


def test_minimax_payload_adaptive_thinking_and_reasoning_split(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.minimax.io/v1")
    monkeypatch.setenv("LLM_THINKING", "adaptive")
    monkeypatch.setenv("LLM_REASONING_SPLIT", "true")

    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        return httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": "{}", "tool_calls": None}}],
                "usage": {},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    client.complete_tools("system", [{"role": "user", "content": "x"}], [])

    assert captured["thinking"] == {"type": "adaptive"}
    assert captured["reasoning_split"] is True


def test_non_minimax_payload_omits_thinking(monkeypatch):
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.openai.com/v1")

    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        return httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": "{}", "tool_calls": None}}],
                "usage": {},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    client.complete_tools("system", [{"role": "user", "content": "x"}], [])

    assert "thinking" not in captured
    assert "reasoning_split" not in captured


def test_real_mode_normalizes_internal_tool_calls_in_history(monkeypatch):
    """Loop stores {id,name,args}; wire format must be OpenAI function tool_calls."""
    monkeypatch.setenv("LLM_MODE", "live")
    monkeypatch.setenv("LLM_API_KEY", "test-key")
    monkeypatch.setenv("LLM_BASE_URL", "https://api.minimaxi.com/v1")
    monkeypatch.setenv("LLM_MODEL", "MiniMax-M3")

    captured: dict = {}
    history = [
        {"role": "user", "content": "analyze"},
        {
            "role": "assistant",
            "content": None,
            "tool_calls": [{"id": "c1", "name": "get_account_view", "args": {}}],
        },
        {"role": "tool", "tool_call_id": "c1", "content": "{}"},
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        return httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": '{"items":[]}', "tool_calls": None}}],
                "usage": {},
            },
        )

    client = ToolLLMClient(http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    client.complete_tools("system", history, [])

    asst = captured["messages"][2]
    assert asst["content"] == ""
    assert asst["tool_calls"][0]["type"] == "function"
    assert asst["tool_calls"][0]["function"]["name"] == "get_account_view"
    assert asst["tool_calls"][0]["function"]["arguments"] == "{}"
    # Original loop history must stay in internal format.
    assert history[1]["content"] is None
    assert history[1]["tool_calls"][0] == {"id": "c1", "name": "get_account_view", "args": {}}
