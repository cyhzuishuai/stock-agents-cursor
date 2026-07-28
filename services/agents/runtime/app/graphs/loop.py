"""Shared LangGraph tool-loop for analyst and portfolio agents.

LangGraph API used (langgraph>=0.2):
- ``StateGraph(TypedDict)`` with nodes ``call_model`` and ``tools``
- ``set_entry_point("call_model")``
- ``add_conditional_edges`` after ``call_model`` → ``tools`` | ``END``
- ``add_edge("tools", "call_model")`` then ``compile().invoke(state)``
"""

from __future__ import annotations

import json
import os
import time
from collections.abc import Callable
from typing import Any, TypedDict

from langgraph.graph import END, StateGraph

from stock_agents_common.llm_tools import ToolLLMClient, extract_json_from_content
from stock_agents_common.schemas import validate
from stock_agents_common.tools import RunContext
from stock_agents_common.trace import append_round, finalize_trace, new_trace, result_preview

ToolFn = Callable[..., dict]


class LoopState(TypedDict, total=False):
    messages: list[dict[str, Any]]
    round_i: int
    stop_reason: str
    result: dict[str, Any] | None
    last_tool_calls: list[dict[str, Any]] | None
    last_content: str | None


def web_search_enabled() -> bool:
    raw = os.environ.get("WEB_SEARCH_ENABLED", "").strip().lower()
    if raw in {"false", "0", "no"}:
        return False
    return True


def max_rounds_for(agent: str, req: dict) -> int:
    limits = req.get("limits") or {}
    if limits.get("max_tool_rounds") is not None:
        return int(limits["max_tool_rounds"])
    env_key = "MAX_TOOL_ROUNDS_ANALYST" if agent == "analyst" else "MAX_TOOL_ROUNDS_PORTFOLIO"
    default = "8" if agent == "analyst" else "3"
    return int(os.environ.get(env_key, default) or default)


def openai_tool_schema(name: str, description: str, parameters: dict[str, Any]) -> dict[str, Any]:
    return {
        "type": "function",
        "function": {
            "name": name,
            "description": description,
            "parameters": parameters,
        },
    }


def execute_tool_call(
    *,
    name: str,
    args: dict[str, Any],
    ctx: RunContext,
    registry: dict[str, ToolFn],
) -> dict[str, Any]:
    fn = registry.get(name)
    started = time.perf_counter()
    if fn is None:
        result: dict[str, Any] = {"ok": False, "error": f"unknown_tool:{name}"}
    else:
        try:
            result = fn(ctx, **(args or {}))
        except Exception as exc:  # noqa: BLE001 — tools degrade
            result = {"ok": False, "error": str(exc)}
    latency_ms = int((time.perf_counter() - started) * 1000)
    ok = bool(result.get("ok")) if isinstance(result, dict) else False
    error = None if ok else (result.get("error") if isinstance(result, dict) else "tool_error")
    return {
        "name": name,
        "ok": ok,
        "latency_ms": latency_ms,
        "result": result,
        "result_preview": result_preview(result),
        "error": error,
    }


def _extract_size_proposals_data(tool_result: dict[str, Any]) -> dict[str, Any] | None:
    if not isinstance(tool_result, dict) or not tool_result.get("ok"):
        return None
    data = tool_result.get("data")
    if isinstance(data, dict) and "proposals" in data:
        return data
    return None


def run_tool_loop(
    *,
    agent: str,
    req: dict[str, Any],
    system: str,
    user_message: str,
    tools_schema: list[dict[str, Any]],
    tool_registry: dict[str, ToolFn],
    result_schema: str,
    align_result: Callable[[dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    baseline: dict[str, Any] | None = None,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
    ensure_size_proposals: bool = False,
) -> dict[str, Any]:
    """Run LangGraph ``call_model`` ↔ ``tools`` until final JSON or max rounds."""
    client = llm_client or ToolLLMClient()
    run_ctx = ctx or RunContext(req=req)
    max_rounds = max_rounds_for(agent, req)
    trace = new_trace(agent)
    usage_totals = {"prompt_tokens": 0, "completion_tokens": 0}

    bag: dict[str, Any] = {
        "baseline": baseline,
        "size_proposals_called": False,
        "last_size_proposals": baseline if isinstance(baseline, dict) and "proposals" in baseline else None,
    }

    if ensure_size_proposals and isinstance(baseline, dict) and "proposals" in baseline:
        # Record deterministic baseline as round 1 so size_proposals always appears in trace.
        bag["size_proposals_called"] = True
        append_round(
            trace,
            {
                "llm": {"model": "deterministic", "latency_ms": 0},
                "assistant": {
                    "content": None,
                    "tool_calls": [{"id": "baseline-size", "name": "size_proposals", "args": {}}],
                },
                "tools": [
                    {
                        "id": "baseline-size",
                        "name": "size_proposals",
                        "ok": True,
                        "latency_ms": 0,
                        "result_preview": result_preview({"ok": True, "data": baseline}),
                        "error": None,
                    }
                ],
            },
        )

    def _fallback_candidate() -> dict[str, Any] | None:
        """Baseline / size proposals, or empty dict for analyst align defaults."""
        candidate = bag.get("last_size_proposals") or bag.get("baseline")
        if candidate is not None:
            return candidate if isinstance(candidate, dict) else None
        # Analyst can fill hold/neutral defaults from watchlist via align_result({}).
        if agent == "analyst" and align_result is not None:
            return {}
        return None

    def _try_candidate(candidate: dict[str, Any] | None, stop_reason: str) -> dict[str, Any] | None:
        if candidate is None:
            return None
        aligned = align_result(candidate, req) if align_result else candidate
        validate(aligned, result_schema)
        return {
            "result": aligned,
            "stop_reason": stop_reason,
            "last_tool_calls": None,
            "last_content": None,
        }

    def call_model(state: LoopState) -> dict[str, Any]:
        round_i = int(state.get("round_i") or 0)
        if round_i >= max_rounds:
            update = _try_candidate(_fallback_candidate(), "max_rounds")
            if update is not None:
                return {**update, "round_i": round_i}
            return {
                "stop_reason": "max_rounds",
                "result": None,
                "last_tool_calls": None,
                "last_content": None,
                "round_i": round_i,
            }

        messages = list(state.get("messages") or [])
        resp = client.complete_tools(system, messages, tools_schema)
        usage = resp.get("usage") or {}
        usage_totals["prompt_tokens"] += int(usage.get("prompt_tokens") or 0)
        usage_totals["completion_tokens"] += int(usage.get("completion_tokens") or 0)

        tool_calls = resp.get("tool_calls")
        content = resp.get("content")
        model_name = (os.environ.get("LLM_MODEL") or "mock").strip() or "mock"

        append_round(
            trace,
            {
                "i": round_i + 1,
                "llm": {"model": model_name, "latency_ms": int(resp.get("latency_ms") or 0)},
                "assistant": {
                    "content": content,
                    "tool_calls": [
                        {"id": tc["id"], "name": tc["name"], "args": tc.get("args") or {}}
                        for tc in (tool_calls or [])
                    ]
                    if tool_calls
                    else [],
                },
                "tools": [],
            },
        )

        if tool_calls:
            # MiniMax multi-turn: keep full assistant message; never send content=null.
            assistant_msg: dict[str, Any] = {
                "role": "assistant",
                "content": content if content is not None else "",
                "tool_calls": tool_calls,
            }
            return {
                "messages": messages + [assistant_msg],
                "round_i": round_i + 1,
                "last_tool_calls": tool_calls,
                "last_content": content,
                "stop_reason": "",
                "result": None,
            }

        parsed = extract_json_from_content(content)

        if parsed is not None:
            aligned = align_result(parsed, req) if align_result else parsed
            validate(aligned, result_schema)
            return {
                "messages": messages,
                "round_i": round_i + 1,
                "last_tool_calls": None,
                "last_content": content,
                "result": aligned,
                "stop_reason": "final",
            }

        # Prefer baseline when present (portfolio); analyst empty non-JSON → hold defaults.
        stop = "final" if bag.get("last_size_proposals") or bag.get("baseline") else "max_rounds"
        update = _try_candidate(_fallback_candidate(), stop)
        if update is not None:
            return {**update, "round_i": round_i + 1, "last_content": content}

        raise ValueError("LLM returned no valid JSON final result")

    def execute_tools(state: LoopState) -> dict[str, Any]:
        tool_calls = state.get("last_tool_calls") or []
        messages = list(state.get("messages") or [])
        tool_trace_entries: list[dict[str, Any]] = []

        for tc in tool_calls:
            name = str(tc.get("name") or "")
            args = tc.get("args") or {}
            if not isinstance(args, dict):
                args = {}
            tc_id = str(tc.get("id") or "")
            executed = execute_tool_call(name=name, args=args, ctx=run_ctx, registry=tool_registry)

            if name == "size_proposals":
                bag["size_proposals_called"] = True
                sized = _extract_size_proposals_data(executed["result"])
                if sized is not None:
                    bag["last_size_proposals"] = sized

            tool_trace_entries.append(
                {
                    "id": tc_id,
                    "name": name,
                    "ok": executed["ok"],
                    "latency_ms": executed["latency_ms"],
                    "result_preview": executed["result_preview"],
                    "error": executed["error"],
                }
            )
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": tc_id,
                    "name": name,
                    "content": json.dumps(executed["result"], ensure_ascii=False, default=str),
                }
            )

        if trace["rounds"]:
            trace["rounds"][-1]["tools"] = tool_trace_entries

        return {
            "messages": messages,
            "last_tool_calls": None,
            "last_content": None,
        }

    def route_after_model(state: LoopState) -> str:
        if state.get("result") is not None:
            return "end"
        if state.get("stop_reason") in {"final", "max_rounds", "timeout", "error"}:
            return "end"
        if state.get("last_tool_calls"):
            return "tools"
        return "end"

    graph = StateGraph(LoopState)
    graph.add_node("call_model", call_model)
    graph.add_node("tools", execute_tools)
    graph.set_entry_point("call_model")
    graph.add_conditional_edges(
        "call_model",
        route_after_model,
        {"tools": "tools", "end": END},
    )
    graph.add_edge("tools", "call_model")
    compiled = graph.compile()

    initial: LoopState = {
        "messages": [{"role": "user", "content": user_message}],
        "round_i": 0,
        "stop_reason": "",
        "result": None,
        "last_tool_calls": None,
        "last_content": None,
    }
    final_state = compiled.invoke(initial)

    stop_reason = str(final_state.get("stop_reason") or "error")
    result = final_state.get("result")

    if ensure_size_proposals and not bag.get("size_proposals_called") and "size_proposals" in tool_registry:
        executed = execute_tool_call(
            name="size_proposals",
            args={},
            ctx=run_ctx,
            registry=tool_registry,
        )
        bag["size_proposals_called"] = True
        sized = _extract_size_proposals_data(executed["result"])
        if sized is not None:
            bag["last_size_proposals"] = sized
        append_round(
            trace,
            {
                "llm": {"model": "deterministic", "latency_ms": 0},
                "assistant": {
                    "content": None,
                    "tool_calls": [{"id": "baseline-size", "name": "size_proposals", "args": {}}],
                },
                "tools": [
                    {
                        "id": "baseline-size",
                        "name": "size_proposals",
                        "ok": executed["ok"],
                        "latency_ms": executed["latency_ms"],
                        "result_preview": executed["result_preview"],
                        "error": executed["error"],
                    }
                ],
            },
        )
        if result is None and sized is not None:
            aligned = align_result(sized, req) if align_result else sized
            validate(aligned, result_schema)
            result = aligned
            stop_reason = "final" if stop_reason not in {"final", "max_rounds"} else stop_reason

    if result is None:
        candidate = _fallback_candidate()
        if candidate is not None:
            aligned = align_result(candidate, req) if align_result else candidate
            validate(aligned, result_schema)
            result = aligned
            if stop_reason not in {"final", "max_rounds", "timeout"}:
                stop_reason = "max_rounds"
        else:
            finalize_trace(trace, "error")
            raise ValueError(f"{agent} graph ended without valid result")

    finalize_trace(trace, stop_reason)
    trace["usage"] = usage_totals
    envelope = {"result": result, "trace": trace}
    validate(envelope, "agent_run_response")
    return envelope
