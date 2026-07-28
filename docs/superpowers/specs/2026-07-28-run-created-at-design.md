# Run CreatedAt on Runs Page — Design Spec

**Date:** 2026-07-28  
**Status:** Approved for implementation  

## Goal

Keep `trade_date` as the US/Eastern **calendar trading day**. Persist and display real **run start time** (`created_at` with hour+minute) on the Runs list and run detail pages so same-day runs are distinguishable.

## Locked decisions

| Topic | Choice |
|-------|--------|
| Approach | Add real `CreatedAt` on `WorkflowRun`; do **not** change `trade_date` semantics |
| API | `GET /api/v1/runs` and `GET /api/v1/runs/:id` return RFC3339 `created_at` |
| Placeholder | Remove `createdAtPlaceholder` for runs (approvals placeholder out of scope) |
| UI list | Keep Trade date column; add **Started** column formatted local `YYYY-MM-DD HH:mm` |
| UI detail | Meta row shows Started next to trade date / status / trigger / strategy |
| Empty/legacy | Zero or missing timestamp → display `—` |
| Timezone | Store UTC in DB (GORM default); format in browser local timezone |
| Docs | Update `packages/contracts/api_dto.md` run detail to include `created_at` |

## Non-goals

- Changing `trade_date` to datetime
- Approvals `created_at` real timestamps
- Overview `latest_run` time fields (optional follow-up)
- Backfilling historical run times from logs

## Acceptance

- New runs get non-empty `created_at` after create
- Runs list and detail show Started with hour and minute
- Mock/smoke tests assert `created_at` is present and non-placeholder for new runs
