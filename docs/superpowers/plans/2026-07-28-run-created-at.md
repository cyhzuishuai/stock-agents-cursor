# Run CreatedAt on Runs Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist real `WorkflowRun.CreatedAt` and show a **Started** (`YYYY-MM-DD HH:mm`) field on Runs list and detail while keeping `trade_date` as calendar-day only.

**Architecture:** Add GORM `CreatedAt` on `WorkflowRun`. List/detail handlers serialize RFC3339 (empty string when zero). Web formats with `Intl`/local date helpers into `YYYY-MM-DD HH:mm`, showing `—` when blank.

**Tech Stack:** Go + GORM, Gin, Next.js (App Router), Vitest, TypeScript

## Global Constraints

- Do **not** change `trade_date` semantics (still `YYYY-MM-DD` US/Eastern trading day)
- Approvals `created_at` placeholder stays out of scope
- Empty/zero timestamps display as `—`
- Store UTC via GORM; format in browser local timezone

## File map

| File | Responsibility |
|------|----------------|
| `services/api/internal/models/workflow.go` | `CreatedAt` field |
| `services/api/internal/httpserver/handlers_runs.go` | Return real `created_at` |
| `services/api/internal/httpserver/api_smoke_test.go` | Assert non-empty `created_at` |
| `packages/contracts/api_dto.md` | Document `created_at` on get-run |
| `apps/web/src/lib/datetime.ts` | Shared format helper |
| `apps/web/src/lib/types.ts` | `RunDetail.created_at` |
| `apps/web/src/app/(shell)/runs/page.tsx` | Started column |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | Started in meta |
| `apps/web/src/app/(shell)/runs/[id]/page.test.tsx` | Detail assertion |
| `apps/web/src/lib/datetime.test.ts` | Helper unit tests |

---

### Task 1: API — persist and return `created_at`

**Files:**
- Modify: `services/api/internal/models/workflow.go`
- Modify: `services/api/internal/httpserver/handlers_runs.go`
- Modify: `services/api/internal/httpserver/api_smoke_test.go`
- Modify: `packages/contracts/api_dto.md`

**Interfaces:**
- Produces: `WorkflowRun.CreatedAt time.Time`; list/detail JSON field `created_at` as RFC3339 string, or `""` when zero

- [ ] **Step 1: Write failing smoke assertion**

In `TestRunsDetailAndTriggerAndCancel`, after creating `run` via GORM and calling `GET /api/v1/runs/{id}`, assert `created_at` is a non-empty string that parses as time. Also in list response for that id, assert the same.

Example addition inside the existing get-run subtest (after unmarshalling detail map):

```go
ca, _ := detail["created_at"].(string)
if ca == "" {
    t.Fatal("created_at empty")
}
if _, err := time.Parse(time.RFC3339, ca); err != nil {
    // also accept RFC3339Nano from encoding/json
    if _, err2 := time.Parse(time.RFC3339Nano, ca); err2 != nil {
        t.Fatalf("created_at not RFC3339: %q err=%v", ca, err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails (or still passes with empty string — expect Fatal on empty)**

Run: `go test ./internal/httpserver -count=1 -run TestRunsDetailAndTriggerAndCancel`

Expected: FAIL with `created_at empty` (current placeholder returns `""`)

- [ ] **Step 3: Implement model + handlers**

`services/api/internal/models/workflow.go`:

```go
package models

import "time"

type WorkflowRun struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TradeDate  string    `gorm:"index" json:"trade_date"`
	Status     string    `gorm:"index" json:"status"`
	ErrorMsg   string    `json:"error_msg"`
	StrategyID *uint     `json:"strategy_id"`
	Trigger    string    `json:"trigger"`
	CreatedAt  time.Time `json:"created_at"`
}
```

In `handlers_runs.go`, add helper and use it in ListRuns + GetRun:

```go
func formatCreatedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
```

ListRuns item:

```go
"created_at": formatCreatedAt(r.CreatedAt),
```

GetRun response — add:

```go
"created_at": formatCreatedAt(run.CreatedAt),
```

Leave `createdAtPlaceholder` for approvals only.

Update `packages/contracts/api_dto.md`:

```markdown
GET /api/v1/runs -> [{ id, trade_date, status, created_at, strategy_id, strategy_name, trigger }]
GET /api/v1/runs/:id -> { id, trade_date, status, created_at, strategy_id, strategy_name, trigger, steps: [...], proposals: [...], orders: [...] }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/httpserver ./internal/models ./internal/workflow -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/models/workflow.go \
  services/api/internal/httpserver/handlers_runs.go \
  services/api/internal/httpserver/api_smoke_test.go \
  packages/contracts/api_dto.md
git commit -m "feat(api): persist WorkflowRun.CreatedAt on list/detail"
```

---

### Task 2: Web — Started column + detail meta

**Files:**
- Create: `apps/web/src/lib/datetime.ts`
- Create: `apps/web/src/lib/datetime.test.ts`
- Modify: `apps/web/src/lib/types.ts`
- Modify: `apps/web/src/app/(shell)/runs/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.test.tsx`

**Interfaces:**
- Consumes: API `created_at` RFC3339 or `""`
- Produces: `formatStartedAt(iso: string | null | undefined): string` → `YYYY-MM-DD HH:mm` or `—`

- [ ] **Step 1: Write failing helper + page tests**

`apps/web/src/lib/datetime.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatStartedAt } from "./datetime";

describe("formatStartedAt", () => {
  it("formats RFC3339 to local YYYY-MM-DD HH:mm", () => {
    const out = formatStartedAt("2026-07-28T09:05:00Z");
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
  });

  it("returns em dash for empty", () => {
    expect(formatStartedAt("")).toBe("—");
    expect(formatStartedAt(undefined)).toBe("—");
  });
});
```

In `page.test.tsx` fixtures, add `created_at: "2026-07-28T13:45:00Z"` and assert the rendered page shows a formatted Started value (substring of `formatStartedAt(...)` or regex `\d{2}:\d{2}` in meta). Also cover empty → `—`.

- [ ] **Step 2: Run tests to verify fail**

Run: `cd apps/web && npm test -- --run src/lib/datetime.test.ts src/app/(shell)/runs/[id]/page.test.tsx`

Expected: FAIL (module/helper missing or UI missing Started)

- [ ] **Step 3: Implement helper + UI**

`apps/web/src/lib/datetime.ts`:

```ts
export function formatStartedAt(iso: string | null | undefined): string {
  if (!iso || !iso.trim()) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${y}-${m}-${day} ${hh}:${mm}`;
}
```

`RunDetail` add `created_at: string;`

Runs list table: add column header `Started`, cell `{formatStartedAt(run.created_at)}`.

Detail meta: `<span>{formatStartedAt(data.created_at)}</span>` (keep trade_date as-is).

- [ ] **Step 4: Run tests**

Run: `cd apps/web && npm test -- --run src/lib/datetime.test.ts src/app/(shell)/runs/[id]/page.test.tsx`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/lib/datetime.ts apps/web/src/lib/datetime.test.ts \
  apps/web/src/lib/types.ts \
  apps/web/src/app/(shell)/runs/page.tsx \
  apps/web/src/app/(shell)/runs/[id]/page.tsx \
  apps/web/src/app/(shell)/runs/[id]/page.test.tsx
git commit -m "feat(web): show Started HH:mm on Runs list and detail"
```

---

## Spec coverage self-review

| Spec item | Task |
|-----------|------|
| `CreatedAt` on WorkflowRun | Task 1 |
| List + detail RFC3339 `created_at` | Task 1 |
| Remove run placeholder | Task 1 |
| Started column + detail meta | Task 2 |
| Empty → `—` | Task 2 |
| Local TZ format | Task 2 |
| api_dto.md | Task 1 |
| Smoke/tests | Task 1 + 2 |

Approvals / overview / trade_date datetime: explicitly non-goals — no tasks.
