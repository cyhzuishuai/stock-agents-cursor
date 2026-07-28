"""Tool-calling LLM client with mock script mode and OpenAI-compatible API.

Round advancement (mock mode)
-----------------------------
Each ``ToolLLMClient`` instance keeps an integer ``_round_index`` starting at 0.
Every call to ``complete_tools`` consumes ``script["rounds"][_round_index]`` and
then increments the index. State is **per client instance**, not derived from
message length — create a new client (or call ``reset()``) for a fresh script run.

Script resolution order:
1. ``MOCK_TOOL_SCRIPT`` env path (if set)
2. Default fixture: ``packages/contracts/fixtures/mock_tool_scripts/analyst.json``
"""

from __future__ import annotations

import json
import os
import re
import time
from pathlib import Path
from typing import Any

import httpx

_THINK_TAG_RE = re.compile(
    r"<(think|thinking|reason|reasoning)\b[^>]*>.*?</\1>",
    re.IGNORECASE | re.DOTALL,
)
_FENCED_JSON_RE = re.compile(
    r"```(?:json)?\s*(\{.*?\}|\[.*?\])\s*```",
    re.IGNORECASE | re.DOTALL,
)


def extract_json_from_content(content: str | dict[str, Any] | None) -> dict[str, Any] | None:
    """Parse assistant content into a JSON object.

    Strips ``<think>...</think>`` (and similar) tags, prefers fenced
    ``json`` markdown blocks, then falls back to ``json.loads`` on the remainder.
    """
    if content is None:
        return None
    if isinstance(content, dict):
        return content
    if not isinstance(content, str):
        return None
    text = content.strip()
    if not text:
        return None

    cleaned = _THINK_TAG_RE.sub("", text).strip()
    if not cleaned:
        return None

    fenced = _FENCED_JSON_RE.search(cleaned)
    candidates: list[str] = []
    if fenced:
        candidates.append(fenced.group(1).strip())
    candidates.append(cleaned)

    # Also try first {...} object substring when fences/plain fail.
    brace = cleaned.find("{")
    if brace >= 0:
        candidates.append(cleaned[brace:])

    seen: set[str] = set()
    for candidate in candidates:
        if candidate in seen:
            continue
        seen.add(candidate)
        try:
            loaded = json.loads(candidate)
        except json.JSONDecodeError:
            continue
        if isinstance(loaded, dict):
            return loaded
    return None


def _find_repo_root() -> Path:
    current = Path(__file__).resolve().parent
    for parent in [current, *current.parents]:
        if (parent / "packages" / "contracts").is_dir():
            return parent
    raise FileNotFoundError("Could not locate repo root (packages/contracts)")


def _is_mock_mode() -> bool:
    return os.environ.get("LLM_MODE", "").strip().lower() == "mock"


def _default_mock_script_path() -> Path:
    return (
        _find_repo_root()
        / "packages"
        / "contracts"
        / "fixtures"
        / "mock_tool_scripts"
        / "analyst.json"
    )


def _resolve_mock_script_path() -> Path:
    override = os.environ.get("MOCK_TOOL_SCRIPT", "").strip()
    if override:
        path = Path(override)
        if not path.is_file():
            raise FileNotFoundError(f"MOCK_TOOL_SCRIPT not found: {path}")
        return path
    path = _default_mock_script_path()
    if not path.is_file():
        raise FileNotFoundError(f"Default mock tool script not found: {path}")
    return path


def _normalize_tool_calls(raw: list[dict[str, Any]] | None) -> list[dict[str, Any]] | None:
    if not raw:
        return None
    normalized: list[dict[str, Any]] = []
    for item in raw:
        args = item.get("args")
        if args is None and "arguments" in item:
            arguments = item["arguments"]
            if isinstance(arguments, str):
                args = json.loads(arguments) if arguments else {}
            else:
                args = arguments or {}
        if args is None:
            args = {}
        normalized.append(
            {
                "id": str(item.get("id", "")),
                "name": str(item.get("name", "")),
                "args": args if isinstance(args, dict) else {},
            }
        )
    return normalized


def _round_to_response(round_data: dict[str, Any], *, latency_ms: int) -> dict[str, Any]:
    if "tool_calls" in round_data and round_data["tool_calls"]:
        return {
            "content": None,
            "tool_calls": _normalize_tool_calls(round_data["tool_calls"]),
            "usage": round_data.get("usage") or {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
            "latency_ms": latency_ms,
        }

    if "content_json" in round_data:
        content = json.dumps(round_data["content_json"], ensure_ascii=False)
    elif "content" in round_data:
        content = round_data["content"]
        if not isinstance(content, str):
            content = json.dumps(content, ensure_ascii=False)
    else:
        content = None

    return {
        "content": content,
        "tool_calls": None,
        "usage": round_data.get("usage") or {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
        "latency_ms": latency_ms,
    }


def _parse_openai_message(message: dict[str, Any], usage: dict[str, Any] | None, latency_ms: int) -> dict[str, Any]:
    raw_calls = message.get("tool_calls") or []
    tool_calls: list[dict[str, Any]] = []
    for call in raw_calls:
        fn = call.get("function") or {}
        arguments = fn.get("arguments") or "{}"
        if isinstance(arguments, str):
            try:
                args = json.loads(arguments) if arguments.strip() else {}
            except json.JSONDecodeError:
                args = {}
        else:
            args = arguments if isinstance(arguments, dict) else {}
        tool_calls.append(
            {
                "id": str(call.get("id", "")),
                "name": str(fn.get("name", "")),
                "args": args,
            }
        )

    content = message.get("content")
    return {
        "content": content,
        "tool_calls": tool_calls or None,
        "usage": usage or {},
        "latency_ms": latency_ms,
    }


class ToolLLMClient:
    """OpenAI-compatible chat completions with tools, plus scripted mock mode."""

    def __init__(self, *, http_client: httpx.Client | None = None) -> None:
        self._http_client = http_client
        self._round_index = 0
        self._script: dict[str, Any] | None = None

    def reset(self) -> None:
        """Reset mock round index and clear cached script (re-read on next call)."""
        self._round_index = 0
        self._script = None

    def complete_tools(
        self,
        system: str,
        messages: list[dict[str, Any]],
        tools_openai_schema: list[dict[str, Any]],
    ) -> dict[str, Any]:
        """Return ``{content, tool_calls, usage, latency_ms}``.

        In mock mode, advances one scripted round per call (see module docstring).
        """
        if _is_mock_mode():
            return self._complete_mock()
        return self._complete_real(system, messages, tools_openai_schema)

    def _load_script(self) -> dict[str, Any]:
        if self._script is None:
            path = _resolve_mock_script_path()
            self._script = json.loads(path.read_text(encoding="utf-8"))
        return self._script

    def _complete_mock(self) -> dict[str, Any]:
        started = time.perf_counter()
        script = self._load_script()
        rounds = script.get("rounds") or []
        if self._round_index >= len(rounds):
            raise IndexError(
                f"Mock tool script exhausted: round_index={self._round_index}, rounds={len(rounds)}"
            )
        round_data = rounds[self._round_index]
        self._round_index += 1
        latency_ms = int((time.perf_counter() - started) * 1000)
        return _round_to_response(round_data, latency_ms=latency_ms)

    def _complete_real(
        self,
        system: str,
        messages: list[dict[str, Any]],
        tools_openai_schema: list[dict[str, Any]],
    ) -> dict[str, Any]:
        api_key = os.environ.get("LLM_API_KEY", "").strip()
        if not api_key:
            raise ValueError("LLM_API_KEY is required when LLM_MODE is not mock")

        base_url = (os.environ.get("LLM_BASE_URL") or "https://api.openai.com/v1").strip().rstrip("/")
        model = (os.environ.get("LLM_MODEL") or "gpt-4o-mini").strip() or "gpt-4o-mini"

        chat_messages: list[dict[str, Any]] = [{"role": "system", "content": system}, *messages]
        payload: dict[str, Any] = {
            "model": model,
            "messages": chat_messages,
        }

        if tools_openai_schema:
            payload["tools"] = tools_openai_schema
        else:
            # Finalize round: ask for a JSON object when no tools are offered.
            payload["response_format"] = {"type": "json_object"}

        # MiniMax OpenAI-compatible extras (thinking / reasoning_split).
        if "minimax" in base_url.lower():
            thinking_mode = (os.environ.get("LLM_THINKING") or "disabled").strip().lower()
            if thinking_mode == "adaptive":
                payload["thinking"] = {"type": "adaptive"}
            else:
                payload["thinking"] = {"type": "disabled"}
            reasoning_split = (os.environ.get("LLM_REASONING_SPLIT") or "").strip().lower()
            if reasoning_split in {"true", "1", "yes"}:
                payload["reasoning_split"] = True

        started = time.perf_counter()
        if self._http_client is not None:
            response = self._http_client.post(
                f"{base_url}/chat/completions",
                headers={"Authorization": f"Bearer {api_key}"},
                json=payload,
            )
        else:
            with httpx.Client(timeout=120.0) as client:
                response = client.post(
                    f"{base_url}/chat/completions",
                    headers={"Authorization": f"Bearer {api_key}"},
                    json=payload,
                )
        latency_ms = int((time.perf_counter() - started) * 1000)

        response.raise_for_status()
        body = response.json()
        message = body["choices"][0]["message"]
        usage = body.get("usage") or {}
        return _parse_openai_message(message, usage, latency_ms)
