# Strategy Scheduler + Runs Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a DB-backed multi-strategy library (one active) that drives US/Eastern pre-open + intraday scheduling, support `auto_reject_breaches` execution, and surface full agent `payload_json` on Runs detail.

**Architecture:** New `strategies` model/service/API; scheduler derives cron ticks from the active strategy and hot-reloads on activate/PATCH; workflow runner accepts `RunParams` (strategy_id, trigger, execution_mode), allows multiple runs per trade date under a global busy Redis lock, and rejects risk breaches without Approvals when mode is `auto_reject_breaches`. Web Settings gains Strategies CRUD; Runs pages show trigger/strategy and expandable step payloads.

**Tech Stack:** Go (Gin, Gorm, robfig/cron), Redis, Next.js App Router, Vitest, PostgreSQL/SQLite tests.

**Spec:** `docs/superpowers/specs/2026-07-28-strategy-scheduler-runs-observability-design.md`

## Global Constraints

- P1 only — no Alpaca account/order routing
- Exactly one `is_active` strategy; system default not deletable
- Execution modes: `auto_reject_breaches` | `require_approval`
- Fill marks remain data-agent close-based
- Regular open fixed at 09:30 America/New_York for pre-open offset
- Do not commit unless the user explicitly asks (Commit steps are optional gates)
- Run relevant Go tests / `cd apps/web && npm test` before claiming a task done

---

## File map

| File | Responsibility |
|------|----------------|
| `services/api/internal/models/strategy.go` | Strategy model |
| `services/api/internal/models/workflow.go` | Add `StrategyID`, `Trigger` on WorkflowRun |
| `services/api/internal/models/proposal.go` | Add `BreachReasonsJSON` |
| `services/api/internal/db/db.go` | AutoMigrate Strategy |
| `services/api/internal/db/seed.go` | Seed 整体策略1 |
| `services/api/internal/strategy/validate.go` | Field validation |
| `services/api/internal/strategy/schedule.go` | Derive pre-open + intraday cron exprs / tick times |
| `services/api/internal/strategy/service.go` | CRUD + activate (transaction) |
| `services/api/internal/httpserver/handlers_strategies.go` | HTTP handlers |
| `services/api/internal/httpserver/router.go` | Register strategy routes; wire SchedulerReloader |
| `services/api/internal/workflow/lock.go` | Global busy lock |
| `services/api/internal/workflow/runner.go` | RunParams, multi-run/day, execution_mode branch |
| `services/api/internal/workflow/steps.go` | Trigger + execution mode constants |
| `services/api/internal/scheduler/scheduler.go` | Strategy-driven jobs + Reload |
| `services/api/cmd/api/main.go` | Wire strategy scheduler |
| `services/api/internal/httpserver/handlers_runs.go` | Expose strategy_id/name/trigger |
| `apps/web/src/lib/types.ts` | Strategy + Run fields |
| `apps/web/src/app/(shell)/settings/page.tsx` | Strategies panel |
| `apps/web/src/app/(shell)/runs/page.tsx` | Trigger + strategy columns |
| `apps/web/src/app/(shell)/runs/[id]/page.tsx` | Expandable payloads |
| `docs/eod-workflow-flowchart.md` | Note strategy-driven cadence |

---

### Task 1: Models, migrate, seed

**Files:**
- Create: `services/api/internal/models/strategy.go`
- Modify: `services/api/internal/models/workflow.go`
- Modify: `services/api/internal/models/proposal.go`
- Modify: `services/api/internal/db/db.go`
- Modify: `services/api/internal/db/seed.go`
- Modify: `services/api/internal/models/models_table_test.go`
- Modify: `services/api/internal/db/seed_test.go` (assert default strategy)
- Test: `go test ./internal/models/ ./internal/db/ -count=1`

**Interfaces:**
- Produces: `models.Strategy` with fields per spec §3.1
- Produces: `WorkflowRun.StrategyID *uint`, `WorkflowRun.Trigger string`
- Produces: `TradeProposal.BreachReasonsJSON string` `json:"breach_reasons_json"`

- [ ] **Step 1: Add Strategy model**

```go
// services/api/internal/models/strategy.go
package models

import "time"

type Strategy struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	Name                  string    `gorm:"uniqueIndex;size:128" json:"name"`
	Description           string    `gorm:"type:text" json:"description"`
	IsSystemDefault       bool      `json:"is_system_default"`
	IsActive              bool      `gorm:"index" json:"is_active"`
	PreOpenMinutes        int       `json:"pre_open_minutes"`
	IntradayEveryMinutes  int       `json:"intraday_every_minutes"`
	IntradayStartET       string    `gorm:"size:5" json:"intraday_start_et"`
	IntradayEndET         string    `gorm:"size:5" json:"intraday_end_et"`
	ExecutionMode         string    `gorm:"size:64" json:"execution_mode"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}
```

- [ ] **Step 2: Extend WorkflowRun and TradeProposal**

```go
// WorkflowRun additions
StrategyID *uint  `json:"strategy_id"`
Trigger    string `json:"trigger"`

// TradeProposal addition
BreachReasonsJSON string `gorm:"type:text" json:"breach_reasons_json"`
```

- [ ] **Step 3: AutoMigrate + seed 整体策略1**

In `db.AutoMigrate`, append `&models.Strategy{}`.

In `db.Seed`, after risk rules:

```go
var stratCount int64
if err := tx.Model(&models.Strategy{}).Count(&stratCount).Error; err != nil {
	return err
}
if stratCount == 0 {
	if err := tx.Create(&models.Strategy{
		Name:                 "整体策略1",
		Description:          "System default: pre-open + hourly intraday; auto-executes within risk limits; rejects breaches without human approval.",
		IsSystemDefault:      true,
		IsActive:             true,
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        "auto_reject_breaches",
	}).Error; err != nil {
		return err
	}
}
```

- [ ] **Step 4: Update model migrate test to include Strategy; assert seed creates active default**

- [ ] **Step 5: Run tests**

Run: `cd services/api && go test ./internal/models/ ./internal/db/ -count=1`  
Expected: PASS

- [ ] **Step 6: Commit (optional)**

```bash
git add services/api/internal/models services/api/internal/db
git commit -m "feat: add strategies model and seed overall strategy 1"
```

---

### Task 2: Schedule derivation + validation

**Files:**
- Create: `services/api/internal/strategy/validate.go`
- Create: `services/api/internal/strategy/schedule.go`
- Create: `services/api/internal/strategy/schedule_test.go`
- Create: `services/api/internal/strategy/validate_test.go`

**Interfaces:**
- Produces: `strategy.ExecutionModeAutoReject = "auto_reject_breaches"`, `ExecutionModeRequireApproval = "require_approval"`
- Produces: `func ValidateStrategyFields(...) error`
- Produces: `type SchedulePlan struct { PreOpenCron string; IntradayCrons []string }`  
  OR list of `{CronExpr, Trigger}` — prefer:

```go
type JobSpec struct {
	CronExpr string // robfig minute-hour-dom-month-dow, Mon-Fri
	Trigger  string // pre_open | intraday
}

func BuildJobSpecs(s models.Strategy) ([]JobSpec, error)
```

- Consumes: Strategy schedule fields from Task 1

- [ ] **Step 1: Write failing tests for default strategy schedule**

```go
func TestBuildJobSpecsDefaultStrategy(t *testing.T) {
	s := models.Strategy{
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
	}
	jobs, err := BuildJobSpecs(s)
	if err != nil {
		t.Fatal(err)
	}
	// Expect pre_open cron "20 9 * * 1-5" and intraday hours 10..15
	var triggers []string
	var exprs []string
	for _, j := range jobs {
		triggers = append(triggers, j.Trigger)
		exprs = append(exprs, j.CronExpr)
	}
	if !contains(exprs, "20 9 * * 1-5") {
		t.Fatalf("missing pre-open: %#v", exprs)
	}
	for _, hour := range []int{10, 11, 12, 13, 14, 15} {
		want := fmt.Sprintf("0 %d * * 1-5", hour)
		if !contains(exprs, want) {
			t.Fatalf("missing %s in %#v", want, exprs)
		}
	}
}
```

Also test: `pre_open_minutes=0` → no pre_open jobs; `intraday_every_minutes=0` → no intraday; invalid HH:MM → error; interval 10 → validation error.

- [ ] **Step 2: Run tests — expect FAIL (undefined)**

Run: `cd services/api && go test ./internal/strategy/ -count=1`

- [ ] **Step 3: Implement ValidateStrategyFields + BuildJobSpecs**

Logic:

- Parse `HH:MM` to minutes from midnight
- Pre-open: openMinutes = 9*60+30; fire = openMinutes - PreOpenMinutes; emit `fmt.Sprintf("%d %d * * 1-5", m%60, m/60)` with Trigger `pre_open`
- Intraday: for t := start; t <= end; t += every → cron at that clock, Trigger `intraday`
- Validate: mode enum; pre_open ≥ 0; intraday every 0 or ≥15; start ≤ end

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Commit (optional)**

```bash
git add services/api/internal/strategy
git commit -m "feat: derive strategy cron job specs from schedule fields"
```

---

### Task 3: Strategy service (CRUD + activate)

**Files:**
- Create: `services/api/internal/strategy/service.go`
- Create: `services/api/internal/strategy/service_test.go`

**Interfaces:**
- Produces:

```go
type Service struct{ DB *gorm.DB }

func (s *Service) List(ctx context.Context) ([]models.Strategy, error)
func (s *Service) Get(ctx context.Context, id uint) (models.Strategy, error)
func (s *Service) Create(ctx context.Context, in CreateInput) (models.Strategy, error)
func (s *Service) Update(ctx context.Context, id uint, in UpdateInput) (models.Strategy, error)
func (s *Service) Activate(ctx context.Context, id uint) (models.Strategy, error)
func (s *Service) Delete(ctx context.Context, id uint) error
func (s *Service) Active(ctx context.Context) (*models.Strategy, error) // nil if none

var ErrNotFound = errors.New("strategy not found")
var ErrForbidden = errors.New("strategy operation forbidden")
var ErrValidation = ... // or return fmt.Errorf wrapping validate
```

- CreateInput/UpdateInput: name, description, schedule fields, execution_mode (no is_system_default / is_active on create except false)

- [ ] **Step 1: Failing tests**

- Create then List includes row with `IsActive=false`, `IsSystemDefault=false`
- Activate A then Activate B → only B active
- Delete system default → `ErrForbidden`
- Delete active → `ErrForbidden`
- Update invalid mode → validation error

Use SQLite via `db.ConnectSQLite` + AutoMigrate + Seed like other packages.

- [ ] **Step 2: Implement Service**

Activate:

```go
return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	if err := tx.Model(&models.Strategy{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
		return err
	}
	res := tx.Model(&models.Strategy{}).Where("id = ?", id).Update("is_active", true)
	...
})
```

Delete: load row; if `IsSystemDefault || IsActive` return `ErrForbidden`.

- [ ] **Step 3: Tests PASS**

Run: `cd services/api && go test ./internal/strategy/ -count=1`

- [ ] **Step 4: Commit (optional)**

```bash
git add services/api/internal/strategy
git commit -m "feat: strategy CRUD and single-active activate"
```

---

### Task 4: Strategy HTTP API

**Files:**
- Create: `services/api/internal/httpserver/handlers_strategies.go`
- Modify: `services/api/internal/httpserver/handlers_helpers.go` (API + RouterDeps: `Strategies *strategy.Service`, `Scheduler Reloader`)
- Modify: `services/api/internal/httpserver/router.go`
- Modify: `services/api/internal/httpserver/api_smoke_test.go` (or new `handlers_strategies_test.go`)

**Interfaces:**
- Produces routes under `authed`:
  - `GET /strategies`, `GET /strategies/:id`, `POST /strategies`, `PATCH /strategies/:id`, `POST /strategies/:id/activate`, `DELETE /strategies/:id`
- Consumes: `strategy.Service`
- Reloader interface:

```go
type SchedulerReloader interface {
	Reload(ctx context.Context) error
}
```

Call `Reload` after successful Activate and after PATCH when resulting row `IsActive`.

Map `ErrForbidden` → 403, `ErrNotFound` → 404, validation → 400.

- [ ] **Step 1: Add handler tests with sqlite + seed + JWT login pattern from smoke tests**

Cover: list includes 整体策略1; create; activate switches; delete default → 403.

- [ ] **Step 2: Implement handlers + wire router + noop reloader in tests**

```go
type noopReloader struct{}
func (noopReloader) Reload(context.Context) error { return nil }
```

- [ ] **Step 3: Tests PASS**

Run: `cd services/api && go test ./internal/httpserver/ -count=1`

- [ ] **Step 4: Commit (optional)**

```bash
git add services/api/internal/httpserver
git commit -m "feat: expose strategy REST API"
```

---

### Task 5: Runner RunParams, busy lock, multi-run/day, auto_reject

**Files:**
- Modify: `services/api/internal/workflow/lock.go`
- Modify: `services/api/internal/workflow/steps.go` (constants)
- Modify: `services/api/internal/workflow/runner.go`
- Modify: `services/api/internal/httpserver/handlers_helpers.go` (`EODRunner` interface)
- Modify: `services/api/internal/httpserver/handlers_runs.go` / `handlers_internal.go`
- Modify: `services/api/internal/scheduler/scheduler.go` (call site — may complete in Task 6)
- Modify: `services/api/internal/workflow/runner_test.go`
- Modify: stubs in `api_smoke_test.go` / `scheduler_test.go`

**Interfaces:**
- Produces:

```go
type RunParams struct {
	TradeDate     string
	Force         bool // retained for API compat; no longer blocks same-day sequential runs
	StrategyID    *uint
	Trigger       string // manual|pre_open|intraday|legacy_eod
	ExecutionMode string // empty → require_approval
}

func (r *Runner) RunEOD(ctx context.Context, params RunParams) (runID uint, err error)
```

Update `EODRunner` interface to match. All callers pass `RunParams`.

- Busy lock key: `eod:run:lock:busy` (replace per-tradeDate lock for acquisition; delete old key helper or keep unused)
- **Remove** the `ErrTradeDateExists` gate for non-force runs (multiple runs per day allowed). Update `TestRunEODRejectsDuplicateTradeDate` → `TestRunEODAllowsSecondRunSameTradeDate` or delete and add busy-lock skip test.
- On create run: set `StrategyID`, `Trigger`
- In fill loop when `!decision.AutoExecute`:
  - if mode == `auto_reject_breaches`: set proposal status `rejected`, `BreachReasonsJSON`, **do not** create Approval; do not set `pendingApprovals`
  - else: existing approval path

Resolve mode: use `params.ExecutionMode` if set; else if StrategyID load strategy; else `require_approval`.

- [ ] **Step 1: Write/adjust failing tests**

1. Breach + `ExecutionMode: auto_reject_breaches` → proposal `rejected`, zero pending approvals, run `executed`
2. Breach + `require_approval` → still pending approval (existing test, ensure still passes)
3. Two RunEOD same trade_date sequential both succeed (second after first completes)

- [ ] **Step 2: Implement lock + RunParams + branch**

- [ ] **Step 3: Fix all compile breakages at call sites (temporary scheduler may pass Trigger legacy_eod)**

- [ ] **Step 4: `go test ./internal/workflow/ ./internal/httpserver/ ./internal/scheduler/ ./internal/approvals/ -count=1` PASS**

- [ ] **Step 5: Commit (optional)**

```bash
git add services/api/internal/workflow services/api/internal/httpserver services/api/internal/scheduler
git commit -m "feat: run params, busy lock, auto-reject breach mode"
```

---

### Task 6: Strategy-driven scheduler + main wiring

**Files:**
- Modify: `services/api/internal/scheduler/scheduler.go`
- Modify: `services/api/internal/scheduler/scheduler_test.go`
- Modify: `services/api/cmd/api/main.go`
- Modify: `deploy/README.md` (note DB strategy authoritative; `EOD_CRON` legacy)

**Interfaces:**
- Produces:

```go
type StrategySource interface {
	Active(ctx context.Context) (*models.Strategy, error)
}

type Scheduler struct {
	// ...
	source StrategySource
	mu     sync.Mutex
}

func New(opts Options) (*Scheduler, error) // Options includes Runner, Location, Now, Source

func (s *Scheduler) Reload(ctx context.Context) error
func (s *Scheduler) Start(ctx context.Context) error
```

`Reload`: mutex; stop existing cron if any; load Active; if nil, register **no** jobs (log); else `BuildJobSpecs` and `AddFunc` each with closure capturing `trigger`, calling:

```go
runner.RunEOD(ctx, workflow.RunParams{
	TradeDate: TradeDate(now, loc),
	StrategyID: &id,
	Trigger: job.Trigger,
	ExecutionMode: strat.ExecutionMode,
})
```

On `ErrLockHeld`, log skip and return nil (do not crash Start loop).

`Start`: initial Reload then block on ctx; on cancel stop cron.

Wire in `main.go`: construct `strategy.Service`, pass as Source; pass Scheduler as `SchedulerReloader` into RouterDeps; start goroutine `sched.Start`.

Handlers already call Reload from Task 4.

- [ ] **Step 1: Tests for BuildJobSpecs integration via Reload**

Use fake Source returning default strategy; after Reload, call internal helper or expose `JobCount()` for tests; assert ≥ 7 jobs (1 pre-open + 6 hourly) OR test `BuildJobSpecs` already covered and only test Reload replaces jobs (call Reload twice with different strategies).

- [ ] **Step 2: Implement Scheduler Reload/Start**

- [ ] **Step 3: Wire main.go**

- [ ] **Step 4: Tests PASS + README note**

- [ ] **Step 5: Commit (optional)**

```bash
git add services/api/internal/scheduler services/api/cmd/api/main.go deploy/README.md
git commit -m "feat: drive EOD scheduler from active strategy"
```

---

### Task 7: Enrich Runs list/detail API

**Files:**
- Modify: `services/api/internal/httpserver/handlers_runs.go`
- Modify smoke/list tests if present

**Interfaces:**
- List/Get include: `strategy_id`, `strategy_name` (lookup by id; empty string if null), `trigger`
- Get continues returning full `steps` including `payload_json`

- [ ] **Step 1: Update GetRun/ListRuns response maps**

```go
strategyName := ""
if run.StrategyID != nil {
	var st models.Strategy
	if err := h.DB.First(&st, *run.StrategyID).Error; err == nil {
		strategyName = st.Name
	}
}
```

- [ ] **Step 2: Manual/smoke assertion that payload_json present on steps**

- [ ] **Step 3: `go test ./internal/httpserver/ -count=1` PASS**

- [ ] **Step 4: Commit (optional)**

```bash
git add services/api/internal/httpserver/handlers_runs.go
git commit -m "feat: expose strategy and trigger on runs API"
```

---

### Task 8: Web types + Settings Strategies panel

**Files:**
- Modify: `apps/web/src/lib/types.ts`
- Modify: `apps/web/src/app/(shell)/settings/page.tsx`
- Create: `apps/web/src/app/(shell)/settings/page.test.tsx` (or `StrategiesPanel.test.tsx`)
- Modify: `apps/web/src/app/globals.css` (minimal form styles if missing — reuse `.btn`, inputs used on login)

**Interfaces:**
- Types:

```ts
export type ExecutionMode = "auto_reject_breaches" | "require_approval";

export interface Strategy {
  id: number;
  name: string;
  description: string;
  is_system_default: boolean;
  is_active: boolean;
  pre_open_minutes: number;
  intraday_every_minutes: number;
  intraday_start_et: string;
  intraday_end_et: string;
  execution_mode: ExecutionMode;
}
```

Use exact snake_case matching API json tags.

- [ ] **Step 1: Failing test — Settings renders Strategies heading and seeded name when API mocked**

- [ ] **Step 2: Implement Strategies section**

Features: list table, Create form toggle, Edit, Activate button, Delete with confirm for non-default inactive, schedule summary helper:

`Pre-open ${n}m · every ${m}m ${start}–${end} ET`

API calls: `GET/POST/PATCH/DELETE /api/v1/strategies`, `POST .../activate`.

- [ ] **Step 3: `cd apps/web && npm test` PASS**

- [ ] **Step 4: Commit (optional)**

```bash
git add apps/web/src
git commit -m "feat: settings strategies CRUD panel"
```

---

### Task 9: Runs list + detail observability UI

**Files:**
- Modify: `apps/web/src/lib/types.ts` (`RunListItem`, `RunDetail`)
- Modify: `apps/web/src/app/(shell)/runs/page.tsx`
- Modify: `apps/web/src/app/(shell)/runs/[id]/page.tsx`
- Create: `apps/web/src/app/(shell)/runs/[id]/page.test.tsx`
- Modify: `apps/web/src/app/globals.css` (`.runs__payload` pre styles)

**Interfaces:**
- RunListItem/RunDetail: `strategy_id: number | null`, `strategy_name: string`, `trigger: string`

- [ ] **Step 1: Failing test — detail page shows pretty JSON when step has payload_json**

Mock `GET /api/v1/runs/1` with one research step payload `{"thesis":"up"}`; assert text `thesis` visible after expanding (or use `getByRole('button', { name: /show payload/i })`).

- [ ] **Step 2: Implement expandable steps**

```tsx
function StepPayload({ raw }: { raw: string }) {
  const [open, setOpen] = useState(false);
  let pretty = raw;
  try {
    pretty = JSON.stringify(JSON.parse(raw), null, 2);
  } catch { /* keep raw */ }
  return (
    <div>
      <button type="button" className="btn btn--ghost" onClick={() => setOpen((v) => !v)}>
        {open ? "Hide payload" : "Show payload"}
      </button>
      {open ? <pre className="runs__payload">{pretty}</pre> : null}
    </div>
  );
}
```

Header shows trigger + strategy_name. List table adds Trigger + Strategy columns.

- [ ] **Step 3: `cd apps/web && npm test` PASS**

- [ ] **Step 4: Commit (optional)**

```bash
git add apps/web/src
git commit -m "feat: show run trigger, strategy, and step payloads"
```

---

### Task 10: Docs sync

**Files:**
- Modify: `docs/eod-workflow-flowchart.md` (short section: strategy-driven schedule; auto_reject path)
- Optionally update Status line on the design spec to `Accepted`

- [ ] **Step 1: Add flowchart note that cadence comes from active strategy; breach may auto-reject**

- [ ] **Step 2: Commit (optional)**

```bash
git add docs
git commit -m "docs: strategy scheduler cadence and auto-reject path"
```

---

## Spec coverage checklist

| Spec requirement | Task |
|------------------|------|
| `strategies` table + seed 整体策略1 | 1 |
| Schedule fields / BuildJobSpecs | 2 |
| CRUD + activate-one | 3–4 |
| Strategy API + reload hook | 4, 6 |
| Pre-open + intraday scheduler | 2, 6 |
| Hot reload on activate/PATCH | 4, 6 |
| `auto_reject_breaches` | 5 |
| `require_approval` regression | 5 |
| Multi-run/day + busy lock | 5 |
| Run strategy_id + trigger | 5, 7 |
| Runs payload UI | 9 |
| Settings Strategies UI | 8 |
| No Alpaca / no per-strategy risk | respected (no tasks) |

## Placeholder / consistency self-review

- No TBD steps; `RunParams` naming consistent across Tasks 5–6
- API field names snake_case aligned with Go json tags
- `EODRunner` signature change called out for all stubs
- Explicit removal of same-day uniqueness conflicting with hourly runs
