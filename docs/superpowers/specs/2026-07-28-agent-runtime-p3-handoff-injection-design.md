# P3 Handoff Injection + Run Memory Pass-through — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation  
**Parent:** `docs/superpowers/specs/2026-07-28-agent-runtime-plan-router-design.md` (§4, §6 P3)

## Goal

Within one workflow run, Go injects Analyst `handoff` and `working_memory` into the Portfolio agent request as **sibling keys** beside `prior_step_outputs.analyst` (result-only). Portfolio plan/act prompts consume those fields. Missing handoff does not fail the workflow.

## Locked decisions

| Topic | Choice |
|-------|--------|
| Injection shape | Parallel keys: keep `prior_step_outputs.analyst` = **result only**; add `analyst_handoff`, `analyst_working_memory` |
| Portfolio consumption | Go injects **and** Portfolio reads fields into plan/act context |
| Missing handoff | Omit keys or empty objects — **do not** fail the run |
| Risk / orders | Continue to use **only** `portfolio.result.proposals` |
| Memory scope | Run-scoped only (no cross-run) |
| Schema | Reuse existing `packages/contracts/agent_handoff.schema.json` (already validated on Analyst emit) |

## Request shape (Portfolio)

```json
{
  "agent": "portfolio",
  "prior_step_outputs": {
    "analyst": { "items": [] },
    "analyst_handoff": {
      "thesis_by_symbol": {},
      "open_questions": [],
      "evidence_refs": [],
      "confidence_notes": ""
    },
    "analyst_working_memory": {
      "notes": [],
      "evidence_refs": [],
      "open_questions": []
    }
  }
}
```

- `size_proposals` / tools keep reading `prior_step_outputs.analyst.items`.
- Keys present only when Analyst envelope included them (or empty objects if implementer prefers uniform shape — either is OK if Portfolio treats missing/empty equivalently).

## Go changes

- Extend envelope decode to capture optional `handoff` and `working_memory` (in addition to `result` / `trace`).
- After Analyst step succeeds: `prior["analyst"] = result`; if handoff present → `prior["analyst_handoff"] = …`; if working_memory present → `prior["analyst_working_memory"] = …`.
- Persist full envelope in `payload_json` unchanged (already stores raw response).

## Python Portfolio changes

- Read `prior_step_outputs.analyst_handoff` / `analyst_working_memory`.
- Include a compact JSON summary in plan and act system/user context so the LLM can use thesis / open questions / evidence refs when sizing.
- Do not require these fields for mock/CI when absent.

## Tests

- Go workflow: stub Analyst envelope with handoff + working_memory; assert Portfolio request `prior_step_outputs` contains sibling keys; assert `analyst` remains result-shaped (`items`).
- Go workflow: Analyst without handoff still reaches Portfolio.
- Portfolio graph / plan_loop unit: when keys present, they appear in built prompt context (or equivalent assertion).

## Non-goals

- Nesting `{ result, handoff, working_memory }` under `prior_step_outputs.analyst`
- Failing the run when handoff is missing
- Cross-run memory
- Changing Risk/order path away from `portfolio.result.proposals`
- New contract schema beyond documenting the sibling key names (handoff schema already exists)

## Success criteria

1. Live/mock workflow: Portfolio HTTP body includes Analyst handoff + working_memory when Analyst emitted them.
2. `prior_step_outputs.analyst` still validates as analyst result usage (`items` for sizing).
3. Portfolio prompts/context include the injected fields when present.
4. Missing handoff does not mark the run failed.
5. Docs: brief note in `docs/product-overview.md` that run memory is Analyst→Portfolio via Go.
