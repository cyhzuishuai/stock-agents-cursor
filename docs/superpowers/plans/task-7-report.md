# Task 7 Report — Go runner chain + marks without data + approvals

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Switched the EOD agent chain to **analyst → portfolio** against a single `AGENT_RUNTIME_URL`, persist `{result,trace}` envelopes, forward only `result` in `prior_step_outputs`, and derive marks without the retired data step.

## Changes

| Path | Change |
|------|--------|
| `services/api/internal/workflow/steps.go` | `StepAnalyst`; `AgentChain()` = analyst (180s) → portfolio; keep legacy step consts |
| `services/api/internal/workflow/runner.go` | Envelope unwrap; `buildAgentSnapshot` + `buildRiskContext`; `agentURL` → `RuntimeURL`; marks = broker then `estimated_notional/qty` |
| `services/api/internal/workflow/runner_test.go` | Two-step runtime stub returning envelopes; marks/approval/nil-broker cases updated |
| `services/api/internal/approvals/service.go` | `loadMarks` stops reading `StepData`; broker marks and/or proposal notional/qty |
| `services/api/internal/approvals/service_test.go` | Drop data-step fixture; marks from proposal |
| `services/api/internal/httpserver/api_smoke_test.go` | Step fixtures use analyst envelope |
| `services/api/cmd/api/main.go` | Wire `Runner.Config` |

## Marks / envelope behavior

1. Persist full raw `{result,trace}` on each step  
2. `prior[step]` = decoded **result** only  
3. Marks: empty → `mergeBrokerMarks` → else `estimated_notional/qty` when qty>0 → else missing-mark error  
4. Approvals cancel/NAV: same mark sources; `ErrMissingFillPrice` if none  

## Verification

```text
cd services/api
go test ./internal/workflow/ ./internal/approvals/ ./internal/httpserver/ -count=1
# ok
go test ./... -count=1
# ok
```

## Remaining test gaps

- No explicit assertion that portfolio request `prior_step_outputs.analyst` is the result object (not the envelope) — covered indirectly by happy-path chain.
- No unit test that approvals prefer broker `CurrentPrice` over proposal-derived marks when both exist.
- No test for empty `RuntimeURL` error path from `agentURL`.
- Legacy five-step historical payloads still readable via kept constants; no dedicated regression for mixed old/new step rows in one DB.

## Commit

```
feat(api): analyst/portfolio chain with tool-loop envelopes
```

No push.
