# Task 8 Report — Web Runs tool timeline

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Run detail steps now parse `{result, trace}` envelopes: compact result summary, collapsible tool trace timeline, `stop_reason` / token usage chips on the step header, and raw JSON fallback. Legacy payloads still use the existing pretty-print toggle.

## Changes

| Path | Change |
|------|--------|
| `apps/web/src/lib/types.ts` | `AgentRunEnvelope`, `AgentTrace`, analyst/portfolio result types |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | `StepResultSummary`, `StepToolTrace`, envelope-aware `StepPayload`; analyst/portfolio step labels |
| `apps/web/src/app/(shell)/runs/[id]/page.test.tsx` | Legacy payload + envelope/tool-trace coverage using contract fixture |

## UI behavior

1. Envelope payloads: result table (analyst side/bias or portfolio proposals) by default  
2. **Show tool trace**: rounds with tool name, args, ok, latency, preview/error  
3. **Show raw payload**: full JSON  
4. Step header shows `stop: {stop_reason}` and token usage when present  
5. Legacy JSON without envelope unchanged (`Show payload`)

## Verification

```text
cd apps/web
npx vitest run "src/app/(shell)/runs/[id]/page.test.tsx"
# 2 passed
```

## Commit

```
feat(web): render agent tool traces on run detail
```

No push.
