# Plan 07 — Go Workflow, Approvals, Scheduler

> **Wave:** 3  
> **Track:** T-GO-WF  
> **Depends on:** Plan 02, 03, 04, 05.4 (data agent URL), Plan 06 (agent URLs; may stub HTTP in tests)  
> **Parallel with:** Plan 08 page tasks (UI can mock APIs)

**Goal:** EOD orchestration with Redis lock, agent HTTP client, risk gate, auto-fill / approvals, REST APIs.

---

### Task 07.1: Agents HTTP client with retry

**Files:**
- Create: `services/api/internal/agentsclient/client.go`
- Test: `services/api/internal/agentsclient/client_test.go`

**Interfaces:**

```go
type Client struct {
	HTTP *http.Client
	DataURL, ResearchURL, DecisionURL, PortfolioURL, RiskURL string
	MaxRetries int
}

func (c *Client) Call(ctx context.Context, baseURL string, body any, timeout time.Duration) (json.RawMessage, error)
```

- Retry on 5xx/timeout up to `MaxRetries` (default 2)

- [ ] **Step 1: Write failing test with Go `net/http/httest` server that returns 500 twice then 200**

```go
func TestCallRetriesThenSucceeds(t *testing.T) {
	// n := 0; server: if n<2 { n++; 500 } else { 200, `{"ok":true}` }
	// client.MaxRetries=2; Call must succeed
}
```

- [ ] **Step 2: Implement Call with retries**

- [ ] **Step 3: Commit** `feat: agents http client with retries`

---

### Task 07.2: Redis run lock

**Files:**
- Create: `services/api/internal/workflow/lock.go`
- Test: `services/api/internal/workflow/lock_test.go` (miniredis)

**Interfaces:**

```go
func AcquireEODLock(ctx context.Context, rdb redis.Cmdable, tradeDate string, ttl time.Duration) (unlock func(), err error)
// ErrLockHeld when busy
```

- [ ] **Step 1: Test double acquire fails**

- [ ] **Step 2: Implement SET NX**

- [ ] **Step 3: Commit** `feat: redis eod run lock`

---

### Task 07.3: Workflow runner state machine (core)

**Files:**
- Create: `services/api/internal/workflow/runner.go`
- Create: `services/api/internal/workflow/steps.go`
- Test: `services/api/internal/workflow/runner_test.go`

**Interfaces:**

```go
type Runner struct {
	DB *gorm.DB
	Agents *agentsclient.Client
	Ledger *ledger.Service
	Risk risk.Engine
	Redis redis.Cmdable
}

func (r *Runner) RunEOD(ctx context.Context, tradeDate string) (runID uint, err error)
```

Behavior:
1. Acquire lock
2. Create `WorkflowRun` status `created`
3. Call agents in order; persist `WorkflowStepResult`; on failure set run `failed` and return (no fills)
4. Parse portfolio proposals → insert `TradeProposal`
5. Call risk agent (store advisory step); evaluate each proposal with Go `risk.Engine`
6. Auto `ledger.ApplyFill` for pass; create `Approval` pending for breaches; set proposal statuses
7. If any pending approval → run `awaiting_approval` else `executed`
8. Build marks from data bars; `UpsertNAV`

Use `net/http/httest` stubs for all five agents in unit/integration test.

- [ ] **Step 1: Test happy path auto-exec one buy**

- [ ] **Step 2: Test agent failure mid-chain → failed, zero orders**

- [ ] **Step 3: Test breach → approval pending, no fill for that proposal**

- [ ] **Step 4: Implement runner**

- [ ] **Step 5: Commit** `feat: eod workflow runner`

---

### Task 07.4: Approvals decide + cancel run

**Files:**
- Create: `services/api/internal/approvals/service.go`
- Create: `services/api/internal/approvals/handlers.go`
- Test: `services/api/internal/approvals/service_test.go`

**Interfaces:**
- `Decide(approvalID, approved|rejected, note, userID)` → on approved fill via ledger; on rejected mark proposal rejected; refresh run status; UpsertNAV
- `CancelRun(runID)` → cancel pending proposals/approvals; status `cancelled`

- [ ] **Step 1: Tests approve/reject/cancel**

- [ ] **Step 2: Implement**

- [ ] **Step 3: Commit** `feat: approval decide and run cancel`

---

### Task 07.5: REST handlers overview/portfolio/runs/settings + wire router

**Files:**
- Create: `services/api/internal/httpserver/handlers_overview.go`
- Create: `services/api/internal/httpserver/handlers_portfolio.go`
- Create: `services/api/internal/httpserver/handlers_runs.go`
- Create: `services/api/internal/httpserver/handlers_settings.go`
- Create: `services/api/internal/httpserver/handlers_internal.go`
- Modify: `services/api/internal/httpserver/router.go`
- Test: `services/api/internal/httpserver/api_smoke_test.go`

**Interfaces:** Match `packages/contracts/api_dto.md` exactly.

- [ ] **Step 1: Smoke tests with sqlite + auth token**

- [ ] **Step 2: Implement handlers**

- [ ] **Step 3: Commit** `feat: rest api overview portfolio runs settings`

---

### Task 07.6: EOD scheduler (US/Eastern)

**Files:**
- Create: `services/api/internal/scheduler/scheduler.go`
- Test: `services/api/internal/scheduler/scheduler_test.go` (time injection)

**Interfaces:**
- Cron expression configurable `EOD_CRON` default `5 21 * * 1-5` (21:05 UTC ≈ after close EST/EDT approximation — document; better: use `America/New_York` location and `30 16 * * 1-5`)
- Prefer **`America/New_York` 16:30 Mon–Fri**
- Also `POST /internal/eod/run` with `X-Internal-Token`

- [ ] **Step 1: Test timezone schedule helper picks weekday**

- [ ] **Step 2: Implement + wire in main**

- [ ] **Step 3: Commit** `feat: eod scheduler and internal trigger`
