"""Plan → act → reflect → finalize LangGraph runtime.

Control flow (locked):

```text
plan_node → act_model
act_model → tools | reflect
tools → reflect
reflect → act_model | plan_node | finalize_node
finalize_node → END
```
"""

from __future__ import annotations

import json
import os
from collections.abc import Callable
from datetime import datetime, timezone
from typing import Any, TypedDict

from langgraph.errors import GraphInterrupt
from langgraph.graph import END, StateGraph
from langgraph.types import Command, interrupt

from stock_agents_common.llm_tools import ToolLLMClient, extract_json_from_content
from stock_agents_common.observability import run_with_tracing
from stock_agents_common.schemas import validate
from stock_agents_common.tools import RunContext
from stock_agents_common.tools.human_input import validate_human_input_args
from stock_agents_common.trace import append_round, finalize_trace, new_trace, result_preview

from app.checkpoint import default_thread_id, get_checkpointer
from app.graphs.loop import ToolFn, execute_tool_call, max_rounds_for
from app.graphs.plan_types import (
    append_evidence_ref,
    empty_working_memory,
    normalize_plan_steps,
    normalize_reflect,
)

HUMAN_INPUT_TOOL = "request_human_input"


class ThreadAlreadyCompleted(RuntimeError):
    """Thread has a completed checkpoint; refuse silent re-run without force_new."""


def _extract_size_proposals_data(tool_result: dict[str, Any]) -> dict[str, Any] | None:
    if not isinstance(tool_result, dict) or not tool_result.get("ok"):
        return None
    data = tool_result.get("data")
    if isinstance(data, dict) and "proposals" in data:
        return data
    return None


def _extract_interrupt_value(payload: Any) -> dict[str, Any]:
    """Pull human_request from LangGraph interrupt return or GraphInterrupt."""
    interrupts: Any = None
    if isinstance(payload, dict) and payload.get("__interrupt__") is not None:
        interrupts = payload["__interrupt__"]
    elif isinstance(payload, GraphInterrupt):
        interrupts = payload.args[0] if payload.args else ()
    elif hasattr(payload, "interrupts"):
        interrupts = getattr(payload, "interrupts")

    if interrupts is None:
        return {}
    seq = list(interrupts) if not isinstance(interrupts, (str, bytes)) else []
    if not seq:
        return {}
    first = seq[0]
    value = getattr(first, "value", first)
    return value if isinstance(value, dict) else {"question": str(value)}


class PlanLoopState(TypedDict, total=False):
    messages: list[dict[str, Any]]
    plan: list[dict[str, Any]]
    current_step_id: str | None
    working_memory: dict[str, Any]
    round_i: int
    stop_reason: str
    result: dict[str, Any] | None
    handoff: dict[str, Any] | None
    last_tool_calls: list[dict[str, Any]] | None
    last_content: str | None
    reflect_decision: str | None


def _utcnow_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _append_event(events: list[dict[str, Any]], event_type: str, **payload: Any) -> None:
    events.append({"type": event_type, "at": _utcnow_iso(), **payload})


def _build_router_snapshot(events: list[dict[str, Any]], trace: dict[str, Any]) -> dict[str, Any]:
    models: list[str] = []
    seen_models: set[str] = set()
    fallback_used_any = False

    for event in events:
        if event.get("type") != "llm":
            continue
        model = str(event.get("model") or "").strip()
        if model and model not in seen_models:
            seen_models.add(model)
            models.append(model)
        if event.get("fallback_used"):
            fallback_used_any = True

    for round_entry in trace.get("rounds") or []:
        llm = round_entry.get("llm")
        if not isinstance(llm, dict):
            continue
        model = str(llm.get("model") or "").strip()
        if model and model not in seen_models:
            seen_models.add(model)
            models.append(model)
        if llm.get("fallback_used"):
            fallback_used_any = True

    return {"fallback_used_any": fallback_used_any, "models": models}


def _first_pending_id(plan: list[dict[str, Any]]) -> str | None:
    for step in plan:
        if step.get("status") == "pending":
            return str(step["id"])
    return None


def _set_step_status(plan: list[dict[str, Any]], step_id: str, status: str) -> list[dict[str, Any]]:
    updated: list[dict[str, Any]] = []
    for step in plan:
        if str(step.get("id")) == step_id:
            updated.append({**step, "status": status})
        else:
            updated.append(dict(step))
    return updated


def _mark_current_in_progress(plan: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], str | None]:
    step_id = _first_pending_id(plan)
    if not step_id:
        return plan, None
    return _set_step_status(plan, step_id, "in_progress"), step_id


def _llm_meta(resp: dict[str, Any]) -> dict[str, Any]:
    router_meta = resp.get("router") if isinstance(resp.get("router"), dict) else {}
    model_name = (
        str(router_meta.get("model") or "").strip()
        or (os.environ.get("LLM_MODEL") or os.environ.get("LLM_PRIMARY_MODEL") or "mock").strip()
        or "mock"
    )
    meta: dict[str, Any] = {
        "model": model_name,
        "latency_ms": int(resp.get("latency_ms") or 0),
    }
    if router_meta:
        meta["provider"] = router_meta.get("provider")
        meta["fallback_used"] = bool(router_meta.get("fallback_used"))
        if router_meta.get("primary_error"):
            meta["primary_error"] = router_meta.get("primary_error")
    return meta


def _accumulate_usage(usage_totals: dict[str, int], resp: dict[str, Any]) -> None:
    usage = resp.get("usage") or {}
    usage_totals["prompt_tokens"] += int(usage.get("prompt_tokens") or 0)
    usage_totals["completion_tokens"] += int(usage.get("completion_tokens") or 0)


def run_plan_loop(
    *,
    agent: str,
    req: dict[str, Any],
    system_plan: str,
    system_act: str,
    system_reflect: str,
    user_message: str,
    tools_schema: list[dict[str, Any]],
    tool_registry: dict[str, ToolFn],
    result_schema: str,
    align_result: Callable[[dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    baseline: dict[str, Any] | None = None,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
    ensure_size_proposals: bool = False,
    build_handoff: Callable[[dict[str, Any], dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    thread_id: str | None = None,
    force_new: bool = False,
) -> dict[str, Any]:
    """Run plan → act ⇄ tools → reflect → finalize; return envelope or interrupted dict."""
    return run_with_tracing(
        f"plan_loop:{agent}",
        lambda: _run_plan_loop_body(
            agent=agent,
            req=req,
            system_plan=system_plan,
            system_act=system_act,
            system_reflect=system_reflect,
            user_message=user_message,
            tools_schema=tools_schema,
            tool_registry=tool_registry,
            result_schema=result_schema,
            align_result=align_result,
            baseline=baseline,
            llm_client=llm_client,
            ctx=ctx,
            ensure_size_proposals=ensure_size_proposals,
            build_handoff=build_handoff,
            thread_id=thread_id,
            force_new=force_new,
            resume_payload=None,
        ),
        metadata={"agent": agent},
    )


def resume_plan_loop(
    thread_id: str,
    human_response: dict[str, Any],
    *,
    agent: str,
    req: dict[str, Any],
    system_plan: str,
    system_act: str,
    system_reflect: str,
    user_message: str,
    tools_schema: list[dict[str, Any]],
    tool_registry: dict[str, ToolFn],
    result_schema: str,
    align_result: Callable[[dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    baseline: dict[str, Any] | None = None,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
    ensure_size_proposals: bool = False,
    build_handoff: Callable[[dict[str, Any], dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Resume a paused plan-loop thread with a human_response (Command.resume)."""
    return run_with_tracing(
        f"plan_loop_resume:{agent}",
        lambda: _run_plan_loop_body(
            agent=agent,
            req=req,
            system_plan=system_plan,
            system_act=system_act,
            system_reflect=system_reflect,
            user_message=user_message,
            tools_schema=tools_schema,
            tool_registry=tool_registry,
            result_schema=result_schema,
            align_result=align_result,
            baseline=baseline,
            llm_client=llm_client,
            ctx=ctx,
            ensure_size_proposals=ensure_size_proposals,
            build_handoff=build_handoff,
            thread_id=thread_id,
            force_new=False,
            resume_payload=human_response if isinstance(human_response, dict) else {"text": str(human_response)},
        ),
        metadata={"agent": agent, "thread_id": thread_id},
    )


def _run_plan_loop_body(
    *,
    agent: str,
    req: dict[str, Any],
    system_plan: str,
    system_act: str,
    system_reflect: str,
    user_message: str,
    tools_schema: list[dict[str, Any]],
    tool_registry: dict[str, ToolFn],
    result_schema: str,
    align_result: Callable[[dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    baseline: dict[str, Any] | None = None,
    llm_client: ToolLLMClient | None = None,
    ctx: RunContext | None = None,
    ensure_size_proposals: bool = False,
    build_handoff: Callable[[dict[str, Any], dict[str, Any], dict[str, Any]], dict[str, Any]] | None = None,
    thread_id: str | None = None,
    force_new: bool = False,
    resume_payload: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Ungated plan-loop body (optional LangSmith wrap lives in ``run_plan_loop``)."""
    client = llm_client or ToolLLMClient()
    run_ctx = ctx or RunContext(req=req)
    max_rounds = max_rounds_for(agent, req)
    trace = new_trace(agent)
    events: list[dict[str, Any]] = []
    usage_totals = {"prompt_tokens": 0, "completion_tokens": 0}

    bag: dict[str, Any] = {
        "baseline": baseline,
        "size_proposals_called": False,
        "last_size_proposals": baseline if isinstance(baseline, dict) and "proposals" in baseline else None,
        "force_finalize": False,
    }

    if ensure_size_proposals and isinstance(baseline, dict) and "proposals" in baseline:
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
        candidate = bag.get("last_size_proposals") or bag.get("baseline")
        if candidate is not None:
            return candidate if isinstance(candidate, dict) else None
        if agent == "analyst" and align_result is not None:
            return {}
        return None

    def _try_align(candidate: dict[str, Any] | None) -> dict[str, Any] | None:
        if candidate is None:
            return None
        aligned = align_result(candidate, req) if align_result else candidate
        validate(aligned, result_schema)
        return aligned

    def plan_node(state: PlanLoopState) -> dict[str, Any]:
        user_content = user_message
        existing_plan = state.get("plan") or []
        # First entry always has the initial user message in state; only treat as
        # re-plan when a prior plan exists or reflect asked for revise_plan.
        is_replan = bool(existing_plan) or state.get("reflect_decision") == "revise_plan"
        if is_replan:
            user_content = (
                f"{user_message}\n\nPrior plan: {json.dumps(existing_plan, ensure_ascii=False)}\n"
                f"Working memory: {json.dumps(state.get('working_memory') or {}, ensure_ascii=False)}"
            )
        resp = client.complete_plan(system_plan, user_content)
        _accumulate_usage(usage_totals, resp)
        raw_steps = resp.get("plan_steps")
        if not raw_steps and isinstance(resp.get("content"), str):
            parsed = extract_json_from_content(resp.get("content"))
            if isinstance(parsed, dict):
                raw_steps = parsed.get("steps") or parsed.get("plan_steps")
        plan = normalize_plan_steps(raw_steps or [])
        plan, current_step_id = _mark_current_in_progress(plan)
        _append_event(events, "plan", plan=plan, current_step_id=current_step_id)
        if current_step_id:
            _append_event(events, "step_start", step_id=current_step_id)
        llm_meta = _llm_meta(resp)
        _append_event(events, "llm", phase="plan", **llm_meta)
        return {
            "plan": plan,
            "current_step_id": current_step_id,
            "working_memory": state.get("working_memory") or empty_working_memory(),
            "reflect_decision": None,
            "last_tool_calls": None,
            "stop_reason": "",
            "result": None,
        }

    def act_model(state: PlanLoopState) -> dict[str, Any]:
        round_i = int(state.get("round_i") or 0)
        if round_i >= max_rounds or bag.get("force_finalize"):
            bag["force_finalize"] = True
            return {
                "round_i": round_i,
                "stop_reason": "max_rounds",
                "last_tool_calls": None,
                "reflect_decision": "finalize",
            }

        messages = list(state.get("messages") or [])
        # Inject current step hint into a lightweight system-adjacent user note when needed.
        act_messages = messages
        current = state.get("current_step_id")
        plan = state.get("plan") or []
        if current:
            step = next((s for s in plan if str(s.get("id")) == current), None)
            if step:
                hint = {
                    "role": "user",
                    "content": (
                        f"[current_step] id={step.get('id')} title={step.get('title')} "
                        f"status={step.get('status')} tool_hint={step.get('tool_hint')}"
                    ),
                }
                # Avoid duplicating the same hint every round: only append if last isn't identical.
                if not messages or messages[-1].get("content") != hint["content"]:
                    act_messages = messages + [hint]

        resp = client.complete_tools(system_act, act_messages, tools_schema)
        _accumulate_usage(usage_totals, resp)
        tool_calls = resp.get("tool_calls")
        content = resp.get("content")
        llm_meta = _llm_meta(resp)
        _append_event(events, "llm", phase="act", round=round_i + 1, **llm_meta)

        append_round(
            trace,
            {
                "i": round_i + 1,
                "llm": llm_meta,
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
            assistant_msg: dict[str, Any] = {
                "role": "assistant",
                "content": content if content is not None else "",
                "tool_calls": tool_calls,
            }
            return {
                "messages": act_messages + [assistant_msg],
                "round_i": round_i + 1,
                "last_tool_calls": tool_calls,
                "last_content": content,
                "stop_reason": "",
                "result": None,
                "reflect_decision": None,
            }

        # No tool_calls: treat content as step note and route to reflect.
        note_messages = act_messages
        if content:
            note_messages = act_messages + [{"role": "assistant", "content": content}]
            memory = dict(state.get("working_memory") or empty_working_memory())
            notes = list(memory.get("notes") or [])
            notes.append(str(content)[:500])
            memory["notes"] = notes
        else:
            memory = state.get("working_memory") or empty_working_memory()

        return {
            "messages": note_messages,
            "round_i": round_i + 1,
            "last_tool_calls": None,
            "last_content": content,
            "working_memory": memory,
            "stop_reason": "",
            "result": None,
            "reflect_decision": None,
        }

    def tools_node(state: PlanLoopState) -> dict[str, Any]:
        tool_calls = state.get("last_tool_calls") or []
        messages = list(state.get("messages") or [])
        memory = dict(state.get("working_memory") or empty_working_memory())
        tool_trace_entries: list[dict[str, Any]] = []

        normal_calls: list[dict[str, Any]] = []
        human_calls: list[dict[str, Any]] = []
        for tc in tool_calls:
            if str(tc.get("name") or "") == HUMAN_INPUT_TOOL:
                human_calls.append(tc)
            else:
                normal_calls.append(tc)

        def _record_tool(
            *,
            tc_id: str,
            name: str,
            ok: bool,
            latency_ms: int,
            result: Any,
            error: Any,
        ) -> None:
            append_evidence_ref(memory, f"{name}:{ok}")
            _append_event(events, "tool", name=name, ok=ok, latency_ms=latency_ms, error=error)
            tool_trace_entries.append(
                {
                    "id": tc_id,
                    "name": name,
                    "ok": ok,
                    "latency_ms": latency_ms,
                    "result_preview": result_preview(result),
                    "error": error,
                }
            )
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": tc_id,
                    "name": name,
                    "content": json.dumps(result, ensure_ascii=False, default=str),
                }
            )

        for tc in normal_calls:
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

            _record_tool(
                tc_id=tc_id,
                name=name,
                ok=bool(executed["ok"]),
                latency_ms=executed["latency_ms"],
                result=executed["result"],
                error=executed["error"],
            )

        # V1: interrupt on first valid human call after normals; extras → failed tools.
        for idx, tc in enumerate(human_calls):
            name = HUMAN_INPUT_TOOL
            args = tc.get("args") or {}
            if not isinstance(args, dict):
                args = {}
            tc_id = str(tc.get("id") or "")
            human_req, err = validate_human_input_args(args)
            if err or human_req is None:
                failed = {"ok": False, "error": err or "question_required"}
                _record_tool(
                    tc_id=tc_id,
                    name=name,
                    ok=False,
                    latency_ms=0,
                    result=failed,
                    error=failed["error"],
                )
                continue
            if idx > 0:
                failed = {"ok": False, "error": "multiple_human_input_unsupported"}
                _record_tool(
                    tc_id=tc_id,
                    name=name,
                    ok=False,
                    latency_ms=0,
                    result=failed,
                    error=failed["error"],
                )
                continue
            # Pause here; on Command(resume=...) this returns the human_response.
            human_response = interrupt(human_req)
            _record_tool(
                tc_id=tc_id,
                name=name,
                ok=True,
                latency_ms=0,
                result=human_response,
                error=None,
            )

        if trace["rounds"]:
            trace["rounds"][-1]["tools"] = tool_trace_entries

        return {
            "messages": messages,
            "working_memory": memory,
            "last_tool_calls": None,
        }

    def reflect_node(state: PlanLoopState) -> dict[str, Any]:
        if bag.get("force_finalize") or state.get("stop_reason") == "max_rounds":
            _append_event(events, "reflect", decision="finalize", reason="max_rounds")
            return {"reflect_decision": "finalize", "stop_reason": state.get("stop_reason") or "max_rounds"}

        messages = list(state.get("messages") or [])
        plan = list(state.get("plan") or [])
        memory = state.get("working_memory") or empty_working_memory()
        reflect_user = {
            "role": "user",
            "content": (
                f"Plan: {json.dumps(plan, ensure_ascii=False)}\n"
                f"Current step: {state.get('current_step_id')}\n"
                f"Working memory: {json.dumps(memory, ensure_ascii=False)}\n"
                "Return reflect JSON."
            ),
        }
        resp = client.complete_reflect(system_reflect, messages + [reflect_user])
        _accumulate_usage(usage_totals, resp)
        raw = resp.get("reflect")
        if not isinstance(raw, dict):
            parsed = extract_json_from_content(resp.get("content"))
            raw = parsed if isinstance(parsed, dict) else {"decision": "continue"}
        try:
            reflect = normalize_reflect(raw)
        except ValueError:
            reflect = {"decision": "continue", "reason": "invalid_reflect"}

        decision = str(reflect["decision"])
        _append_event(
            events,
            "reflect",
            decision=decision,
            step_id=reflect.get("step_id"),
            reason=reflect.get("reason"),
            **_llm_meta(resp),
        )

        updates: dict[str, Any] = {
            "reflect_decision": decision,
            "last_tool_calls": None,
        }

        if decision == "mark_step_done":
            step_id = str(reflect.get("step_id") or state.get("current_step_id") or "")
            if step_id:
                plan = _set_step_status(plan, step_id, "done")
            plan, next_id = _mark_current_in_progress(plan)
            updates["plan"] = plan
            updates["current_step_id"] = next_id
            if next_id:
                _append_event(events, "step_start", step_id=next_id)
        elif decision == "revise_plan":
            patch = reflect.get("plan_patch")
            patched = False
            if isinstance(patch, list):
                try:
                    plan = normalize_plan_steps(patch)
                    plan, next_id = _mark_current_in_progress(plan)
                    updates["plan"] = plan
                    updates["current_step_id"] = next_id
                    if next_id:
                        _append_event(events, "step_start", step_id=next_id)
                    _append_event(
                        events,
                        "plan",
                        plan=plan,
                        current_step_id=next_id,
                        source="plan_patch",
                    )
                    patched = True
                except ValueError:
                    patched = False
            if patched:
                # List patch applied — do not call complete_plan; resume act.
                updates["reflect_decision"] = "continue"
            # else: keep revise_plan → plan_node for full LLM re-plan
        elif decision == "finalize":
            updates["stop_reason"] = "final"
        # continue: keep current step, act again

        return updates

    def finalize_node(state: PlanLoopState) -> dict[str, Any]:
        stop_reason = str(state.get("stop_reason") or "final")
        if bag.get("force_finalize") and stop_reason not in {"final", "max_rounds", "timeout", "error"}:
            stop_reason = "max_rounds"

        content = state.get("last_content")
        candidate = extract_json_from_content(content)
        result: dict[str, Any] | None = None
        try:
            if candidate is not None:
                result = _try_align(candidate)
        except Exception:  # noqa: BLE001 — repair path
            result = None

        if result is None:
            # Same-model repair once — never switch provider (same client instance).
            repair_system = (
                f"Fix JSON to schema '{result_schema}'. Return ONLY valid JSON object. "
                "Do not call tools."
            )
            repair_messages = list(state.get("messages") or [])
            if content:
                repair_messages = repair_messages + [
                    {
                        "role": "user",
                        "content": f"Previous invalid content:\n{content}\n\nReturn corrected JSON only.",
                    }
                ]
            else:
                repair_messages = repair_messages + [
                    {
                        "role": "user",
                        "content": (
                            f"No valid {result_schema} JSON was produced. "
                            "Return a corrected JSON object matching the schema only."
                        ),
                    }
                ]
            try:
                resp = client.complete_tools(repair_system, repair_messages, [])
                _accumulate_usage(usage_totals, resp)
                repaired = extract_json_from_content(resp.get("content"))
                if repaired is not None:
                    result = _try_align(repaired)
                _append_event(events, "llm", phase="repair", **_llm_meta(resp))
            except Exception:  # noqa: BLE001
                result = None

        if result is None:
            # Fallback: baseline → final; analyst hold defaults → max_rounds.
            try:
                result = _try_align(_fallback_candidate())
                if result is not None:
                    if bag.get("last_size_proposals") or bag.get("baseline"):
                        if stop_reason not in {"final", "max_rounds", "timeout"}:
                            stop_reason = "final"
                    else:
                        stop_reason = "max_rounds"
            except Exception:  # noqa: BLE001
                result = None

        if result is None:
            _append_event(events, "finalize", ok=False, stop_reason="error")
            return {
                "result": None,
                "stop_reason": "error",
                "handoff": None,
            }

        handoff: dict[str, Any] | None = None
        memory = state.get("working_memory") or empty_working_memory()
        if build_handoff is not None:
            built = build_handoff(result, memory, req)
            if built:
                validate(built, "agent_handoff")
                handoff = built

        # Spec event order: handoff before finalize on success.
        if handoff:
            _append_event(events, "handoff", handoff_preview=result_preview(handoff))
        _append_event(events, "finalize", ok=True, stop_reason=stop_reason)
        return {
            "result": result,
            "stop_reason": stop_reason,
            "handoff": handoff,
            "working_memory": memory,
            "plan": state.get("plan") or [],
        }

    def route_after_act(state: PlanLoopState) -> str:
        if bag.get("force_finalize") or state.get("reflect_decision") == "finalize":
            return "finalize"
        if state.get("last_tool_calls"):
            return "tools"
        return "reflect"

    def route_after_reflect(state: PlanLoopState) -> str:
        decision = state.get("reflect_decision") or "continue"
        if decision == "finalize" or bag.get("force_finalize"):
            return "finalize"
        if decision == "revise_plan":
            return "plan"
        # continue / mark_step_done → act again (even if no pending: produce final content)
        return "act"

    graph = StateGraph(PlanLoopState)
    graph.add_node("plan", plan_node)
    graph.add_node("act_model", act_model)
    graph.add_node("tools", tools_node)
    graph.add_node("reflect", reflect_node)
    graph.add_node("finalize", finalize_node)
    graph.set_entry_point("plan")
    graph.add_edge("plan", "act_model")
    graph.add_conditional_edges(
        "act_model",
        route_after_act,
        {"tools": "tools", "reflect": "reflect", "finalize": "finalize"},
    )
    graph.add_edge("tools", "reflect")
    graph.add_conditional_edges(
        "reflect",
        route_after_reflect,
        {"act": "act_model", "plan": "plan", "finalize": "finalize"},
    )
    graph.add_edge("finalize", END)
    compiled = graph.compile(checkpointer=get_checkpointer())

    tid = thread_id or default_thread_id(str(req.get("run_id") or "local"), agent)
    config: dict[str, Any] = {"configurable": {"thread_id": tid}}

    def _interrupted_envelope(human_request: dict[str, Any]) -> dict[str, Any]:
        finalize_trace(trace, "interrupted")
        trace["usage"] = usage_totals
        trace["router"] = _build_router_snapshot(events, trace)
        trace["events"] = events
        trace["stop_reason"] = "interrupted"
        return {
            "status": "interrupted",
            "thread_id": tid,
            "human_request": human_request,
            "trace": trace,
        }

    initial: PlanLoopState = {
        "messages": [{"role": "user", "content": user_message}],
        "plan": [],
        "current_step_id": None,
        "working_memory": empty_working_memory(),
        "round_i": 0,
        "stop_reason": "",
        "result": None,
        "handoff": None,
        "last_tool_calls": None,
        "last_content": None,
        "reflect_decision": None,
    }

    try:
        if resume_payload is not None:
            final_state = compiled.invoke(Command(resume=resume_payload), config)
        else:
            if not force_new:
                existing = compiled.get_state(config)
                if existing.values and not existing.next:
                    raise ThreadAlreadyCompleted(f"thread_already_completed:{tid}")
            final_state = compiled.invoke(initial, config)
    except GraphInterrupt as gi:
        return _interrupted_envelope(_extract_interrupt_value(gi))

    if isinstance(final_state, dict) and final_state.get("__interrupt__"):
        return _interrupted_envelope(_extract_interrupt_value(final_state))

    stop_reason = str(final_state.get("stop_reason") or "error")
    result = final_state.get("result")
    memory = final_state.get("working_memory") or empty_working_memory()
    plan = final_state.get("plan") or []
    handoff = final_state.get("handoff")

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
            append_evidence_ref(memory, f"size_proposals:{bool(executed['ok'])}")
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
            try:
                result = _try_align(sized)
                stop_reason = "final" if stop_reason not in {"final", "max_rounds"} else stop_reason
            except Exception:  # noqa: BLE001
                pass

    if result is None:
        try:
            result = _try_align(_fallback_candidate())
            if result is not None and stop_reason not in {"final", "max_rounds", "timeout"}:
                stop_reason = "max_rounds"
        except Exception:  # noqa: BLE001
            result = None

    if result is None:
        finalize_trace(trace, "error")
        trace["events"] = events
        trace["plan"] = plan
        trace["working_memory"] = memory
        raise ValueError(f"{agent} plan loop ended without valid result")

    finalize_trace(trace, stop_reason)
    trace["usage"] = usage_totals
    trace["router"] = _build_router_snapshot(events, trace)
    trace["events"] = events
    trace["plan"] = plan
    trace["working_memory"] = memory

    envelope: dict[str, Any] = {"result": result, "trace": trace, "working_memory": memory}
    if handoff:
        envelope["handoff"] = handoff
    validate(envelope, "agent_run_response")
    return envelope
