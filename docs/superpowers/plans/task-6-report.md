# Task 6 Report — Go config, agents client, snapshot builder

**Branch:** `feature/agent-runtime-tool-loop`  
**Date:** 2026-07-28  
**Status:** DONE

## Summary

Added `AGENT_RUNTIME_URL` config + `agentsclient.RuntimeURL`, and Alpaca-backed `AgentAccountSnapshot` / `AgentRiskContext` builders for upcoming runner wiring (Task 7). Old per-agent URL fields and `AgentChain` left unchanged.

## Changes

| Path | Change |
|------|--------|
| `services/api/internal/config/config.go` | `AgentRuntimeURL` from `AGENT_RUNTIME_URL` |
| `services/api/internal/config/config_test.go` | Load test for runtime URL |
| `services/api/internal/agentsclient/client.go` | `RuntimeURL` field (old URLs kept) |
| `services/api/cmd/api/main.go` | Wire `RuntimeURL: cfg.AgentRuntimeURL` |
| `services/api/internal/workflow/agent_snapshot.go` | `AgentAccountSnapshot`, `buildAgentSnapshot`, `buildRiskContext` |
| `services/api/internal/workflow/agent_snapshot_test.go` | Fake broker snapshot + risk_context JSON shape tests |

## Snapshot / risk shape

- Snapshot: `cash`, `equity`, `currency=USD`, `positions[]`, `open_orders[]` via `GetAccount` / `ListPositions` / `ListOrders(ctx,"open")`
- Risk: `execution_mode` + `rules.{max_order_notional,max_single_name_weight,min_cash_ratio}` from config
- Dedicated `AgentAccountSnapshot` — does **not** change `ledger.AccountSnapshot`

## Verification

```text
cd services/api
go test ./internal/config ./internal/agentsclient ./internal/workflow -count=1
# ok
```

## Commit

```
feat(api): agent runtime URL and Alpaca agent snapshot
```

No push.
