"""Tool-calling LLM client with mock script mode and OpenAI-compatible API.

Round advancement (mock mode)
-----------------------------
Each ``ToolLLMClient`` instance keeps:

- ``_round_index`` — ``complete_tools`` consumes ``script["rounds"]`` entries
- ``_reflect_index`` — ``complete_reflect`` consumes ``script["reflect"]`` entries
- ``_plan_consumed`` — ``complete_plan`` reads ``script["plan"]`` once

State is **per client instance**, not derived from message length — create a new
client (or call ``reset()``) for a fresh script run. Plan/reflect never advance
``_round_index``.

Script resolution order:
1. ``MOCK_TOOL_SCRIPT`` env path (if set)
2. Default fixture: ``packages/contracts/fixtures/mock_tool_scripts/analyst.json``
"""

from __future__ import annotations

import copy
import json
import os
import re
import time
from pathlib import Path
from typing import Any

import httpx

from stock_agents_common.model_router import ProviderConfig, chat_completions

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


def _coerce_pseudo_tool_calls(content: Any) -> list[dict[str, Any]] | None:
    """MiniMax sometimes emits tool calls as JSON in content instead of tool_calls."""
    parsed = extract_json_from_content(content if isinstance(content, str) else None)
    if not isinstance(parsed, dict):
        return None
    # Single call: {"name": "...", "arguments": {...}} or {"name","args"}
    if "name" in parsed and ("arguments" in parsed or "args" in parsed):
        args = parsed.get("args")
        if args is None:
            arguments = parsed.get("arguments")
            if isinstance(arguments, str):
                try:
                    args = json.loads(arguments) if arguments.strip() else {}
                except json.JSONDecodeError:
                    args = {}
            else:
                args = arguments if isinstance(arguments, dict) else {}
        return [
            {
                "id": str(parsed.get("id") or "pseudo_0"),
                "name": str(parsed.get("name") or ""),
                "args": args if isinstance(args, dict) else {},
            }
        ]
    # Batch: {"tool_calls":[...]} or {"calls":[...]}
    batch = parsed.get("tool_calls") or parsed.get("calls")
    if isinstance(batch, list) and batch:
        return _normalize_tool_calls(
            [
                {
                    "id": item.get("id") or f"pseudo_{i}",
                    "name": (item.get("function") or {}).get("name") or item.get("name"),
                    "args": item.get("args")
                    or item.get("arguments")
                    or (item.get("function") or {}).get("arguments"),
                }
                for i, item in enumerate(batch)
                if isinstance(item, dict)
            ]
        )
    return None


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
    if not tool_calls:
        pseudo = _coerce_pseudo_tool_calls(content)
        if pseudo:
            tool_calls = pseudo
            # Keep content for history; loop treats tool_calls as authoritative.
    return {
        "content": content,
        "tool_calls": tool_calls or None,
        "usage": usage or {},
        "latency_ms": latency_ms,
    }


def _mock_usage() -> dict[str, int]:
    return {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}


def _plan_steps_from_script(plan_data: Any) -> list[dict[str, Any]]:
    """Extract plan steps from script ``plan`` section (raw, lightly shaped)."""
    if isinstance(plan_data, dict):
        steps = plan_data.get("steps", [])
    elif isinstance(plan_data, list):
        steps = plan_data
    else:
        steps = []
    if not isinstance(steps, list):
        return []
    return [copy.deepcopy(s) for s in steps if isinstance(s, dict)]


class ToolLLMClient:
    """OpenAI-compatible chat completions with tools, plus scripted mock mode."""

    def __init__(self, *, http_client: httpx.Client | None = None) -> None:
        self._http_client = http_client
        self._round_index = 0
        self._reflect_index = 0
        self._plan_consumed = False
        self._script: dict[str, Any] | None = None

    def reset(self) -> None:
        """Reset mock indices and clear cached script (re-read on next call)."""
        self._round_index = 0
        self._reflect_index = 0
        self._plan_consumed = False
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

    def complete_plan(self, system: str, user: str) -> dict[str, Any]:
        """Return ``{content, plan_steps, usage, latency_ms, router?}``.

        Mock: read ``script["plan"]`` once (does not consume ``rounds``).
        Live: chat completions without tools; parse JSON ``{steps:[...]}``.
        """
        if _is_mock_mode():
            return self._complete_plan_mock()
        return self._complete_plan_real(system, user)

    def complete_reflect(self, system: str, messages: list[dict[str, Any]]) -> dict[str, Any]:
        """Return ``{content, reflect, usage, latency_ms, router?}``.

        Mock: consume next ``script["reflect"]`` entry (separate index).
        Live: chat completions without tools; parse JSON reflect object.
        """
        if _is_mock_mode():
            return self._complete_reflect_mock()
        return self._complete_reflect_real(system, messages)

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

    def _complete_plan_mock(self) -> dict[str, Any]:
        started = time.perf_counter()
        if self._plan_consumed:
            raise IndexError("Mock plan already consumed; call reset() for a fresh run")
        script = self._load_script()
        if "plan" not in script:
            raise KeyError("Mock tool script missing 'plan' section")
        plan_steps = _plan_steps_from_script(script["plan"])
        self._plan_consumed = True
        latency_ms = int((time.perf_counter() - started) * 1000)
        content = json.dumps({"steps": plan_steps}, ensure_ascii=False)
        return {
            "content": content,
            "plan_steps": plan_steps,
            "usage": _mock_usage(),
            "latency_ms": latency_ms,
        }

    def _complete_reflect_mock(self) -> dict[str, Any]:
        started = time.perf_counter()
        script = self._load_script()
        reflect_list = script.get("reflect") or []
        if self._reflect_index >= len(reflect_list):
            raise IndexError(
                f"Mock reflect script exhausted: "
                f"reflect_index={self._reflect_index}, reflect={len(reflect_list)}"
            )
        reflect = copy.deepcopy(reflect_list[self._reflect_index])
        self._reflect_index += 1
        latency_ms = int((time.perf_counter() - started) * 1000)
        content = json.dumps(reflect, ensure_ascii=False) if isinstance(reflect, dict) else str(reflect)
        return {
            "content": content,
            "reflect": reflect if isinstance(reflect, dict) else {"decision": "finalize", "reason": "invalid"},
            "usage": _mock_usage(),
            "latency_ms": latency_ms,
        }

    def _complete_plan_real(self, system: str, user: str) -> dict[str, Any]:
        parsed_resp = self._complete_real(system, [{"role": "user", "content": user}], tools_openai_schema=[])
        extracted = extract_json_from_content(parsed_resp.get("content"))
        plan_steps: list[dict[str, Any]] = []
        if isinstance(extracted, dict):
            plan_steps = _plan_steps_from_script(extracted)
        return {
            "content": parsed_resp.get("content"),
            "plan_steps": plan_steps,
            "usage": parsed_resp.get("usage") or {},
            "latency_ms": parsed_resp.get("latency_ms", 0),
            **({"router": parsed_resp["router"]} if "router" in parsed_resp else {}),
        }

    def _complete_reflect_real(self, system: str, messages: list[dict[str, Any]]) -> dict[str, Any]:
        parsed_resp = self._complete_real(system, messages, tools_openai_schema=[])
        extracted = extract_json_from_content(parsed_resp.get("content"))
        reflect: dict[str, Any] = extracted if isinstance(extracted, dict) else {}
        return {
            "content": parsed_resp.get("content"),
            "reflect": reflect,
            "usage": parsed_resp.get("usage") or {},
            "latency_ms": parsed_resp.get("latency_ms", 0),
            **({"router": parsed_resp["router"]} if "router" in parsed_resp else {}),
        }

    def _complete_real(
        self,
        system: str,
        messages: list[dict[str, Any]],
        tools_openai_schema: list[dict[str, Any]],
    ) -> dict[str, Any]:
        # Deep-copy so normalizing for the wire format does not mutate loop state.
        chat_messages: list[dict[str, Any]] = [
            {"role": "system", "content": system},
            *[copy.deepcopy(m) for m in messages],
        ]
        payload: dict[str, Any] = {
            "messages": chat_messages,
        }
        if tools_openai_schema:
            payload["tools"] = tools_openai_schema

        # Normalize message content and tool_calls for OpenAI-compatible providers.
        for msg in chat_messages:
            if msg.get("role") == "assistant" and msg.get("content") is None:
                msg["content"] = ""
            raw_calls = msg.get("tool_calls")
            if msg.get("role") == "assistant" and isinstance(raw_calls, list) and raw_calls:
                normalized_calls: list[dict[str, Any]] = []
                for tc in raw_calls:
                    if not isinstance(tc, dict):
                        continue
                    if "function" in tc:
                        normalized_calls.append(tc)
                        continue
                    # Internal loop format: {id, name, args}
                    args = tc.get("args") if isinstance(tc.get("args"), dict) else {}
                    normalized_calls.append(
                        {
                            "id": str(tc.get("id") or ""),
                            "type": "function",
                            "function": {
                                "name": str(tc.get("name") or ""),
                                "arguments": json.dumps(args, ensure_ascii=False),
                            },
                        }
                    )
                msg["tool_calls"] = normalized_calls

        def prepare(provider: ProviderConfig, base_payload: dict[str, Any]) -> dict[str, Any]:
            p = copy.deepcopy(base_payload)
            if "minimax" in provider.base_url.lower():
                thinking_mode = (os.environ.get("LLM_THINKING") or "disabled").strip().lower()
                if thinking_mode == "adaptive":
                    p["thinking"] = {"type": "adaptive"}
                else:
                    p["thinking"] = {"type": "disabled"}
                reasoning_split = (os.environ.get("LLM_REASONING_SPLIT") or "").strip().lower()
                if reasoning_split in {"true", "1", "yes"}:
                    p["reasoning_split"] = True
            elif not p.get("tools"):
                # Finalize round: ask for a JSON object when no tools are offered.
                # MiniMax often ignores/quirks on response_format; rely on prompt + extract_json.
                p["response_format"] = {"type": "json_object"}
            return p

        started = time.perf_counter()
        try:
            router = chat_completions(
                payload=payload,
                http_client=self._http_client,
                timeout=180.0,
                prepare_payload=prepare,
            )
        except ValueError:
            raise
        except httpx.HTTPError as exc:
            raise ValueError(f"LLM request failed: {exc}") from exc
        latency_ms = int((time.perf_counter() - started) * 1000)

        response = router.response
        if response.status_code >= 400:
            detail = (response.text or "")[:800]
            raise ValueError(f"LLM HTTP {response.status_code}: {detail}")
        body = response.json()
        message = body["choices"][0]["message"]
        usage = body.get("usage") or {}
        parsed = _parse_openai_message(message, usage, latency_ms)
        parsed["router"] = {
            "provider": router.provider,
            "model": router.model,
            "fallback_used": router.fallback_used,
            "primary_error": router.primary_error,
        }
        return parsed
