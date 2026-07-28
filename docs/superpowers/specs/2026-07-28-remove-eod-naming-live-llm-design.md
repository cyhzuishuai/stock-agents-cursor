# Remove EOD Naming + MiniMax Live Compatibility — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation  

## Goal

Hard-rename all user-facing and code `eod` workflow naming to strategy **run trigger** semantics; sync product docs with agent-runtime; fix MiniMax-M3 tool-loop JSON/`think` compatibility; add live-LLM e2e on new paths.

## Locked decisions

| Topic | Choice |
|-------|--------|
| API | `POST /api/v1/runs/trigger`, `POST /internal/runs/trigger` — **no** `/eod` aliases |
| Token env | `INTERNAL_RUN_TOKEN` only |
| Lock Redis key | `workflow:run:lock:busy` |
| Entry method | `RunWorkflow` / `WorkflowRunner` |
| Triggers | `manual` \| `pre_open` \| `intraday` (drop writing `legacy_eod`) |
| Live model | `MiniMax-M3` |
| Live e2e | `deploy/e2e_api_live_llm.ps1` via `/runs/trigger` |

## Non-goals

Keep historical `docs/superpowers/plans/*` untouched unless they block builds. No CI default live LLM.
