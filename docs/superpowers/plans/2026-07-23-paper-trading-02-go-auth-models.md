# Plan 02 — Go Auth, Models, Seed

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or executing-plans.
>
> **Wave:** 1  
> **Track:** T-GO-CORE  
> **Depends on:** Plan 01 complete  
> **Parallel with:** Plan 05 Tasks 05.1–05.2 (common only), Plan 08 Task 08.1 (web scaffold)

**Goal:** Bootable Go API with Postgres models, migrations via AutoMigrate, admin bootstrap, JWT login.

**Architecture:** `cmd/api/main.go` loads config, connects DB, registers Gin routes under `/api/v1/auth/*`.

**Tech Stack:** Go 1.22+, Gin, Gorm, postgres driver, golang-jwt, bcrypt.

## Global Constraints

Module path: set in Task 02.1 as `github.com/cyh/stock-agents/services/api` (if changed, update master note).  
Defaults from spec §13. Do not implement ledger/risk/workflow here.

---

### Task 02.1: Go module + config + healthz

**Files:**
- Create: `services/api/go.mod`
- Create: `services/api/cmd/api/main.go`
- Create: `services/api/internal/config/config.go`
- Create: `services/api/internal/httpserver/router.go`
- Test: `services/api/internal/config/config_test.go`

**Interfaces:**
- Consumes: `deploy/env.example` key names
- Produces: `config.Load() (*Config, error)`, `GET /healthz` → `{"status":"ok"}`

- [ ] **Step 1: Write failing config test**

```go
package config_test

import (
	"os"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/config"
)

func TestLoadRequiresJWTSecret(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://x")
	os.Setenv("REDIS_URL", "redis://x")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET missing")
	}
}
```

- [ ] **Step 2: Run test — expect fail**

Run: `cd services/api && go test ./internal/config/ -v`  
Expected: FAIL package not found / undefined

- [ ] **Step 3: Implement `config.Config` + `Load`**

Fields: `DatabaseURL`, `RedisURL`, `JWTSecret`, `AdminUsername`, `AdminPassword`, `InitialCash float64`, `Watchlist []string`, risk floats, agent URLs, `InternalEODToken`, `MarketDataProvider`.

Parse `WATCHLIST` as comma-separated. `INITIAL_CASH` default `100000`.

- [ ] **Step 4: Implement minimal Gin `GET /healthz` and main**

- [ ] **Step 5: Re-run test — PASS; commit**

```bash
git add services/api
git commit -m "feat: go module config and healthz"
```

---

### Task 02.2: Gorm models for all tables

**Files:**
- Create: `services/api/internal/models/models.go` (split later if huge; V1 one file OK under 400 lines — prefer split):
  - `user.go`, `account.go`, `position.go`, `order.go`, `watchlist.go`, `workflow.go`, `proposal.go`, `approval.go`, `risk_config.go`, `nav.go`
- Test: `services/api/internal/models/models_table_test.go`

**Interfaces:**
- Produces: structs matching spec §6.2 table list with JSON tags aligned to `api_dto.md` where exposed

Required models & key fields:

```go
type User struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"uniqueIndex;size:64"`
	PasswordHash string
}

type Account struct {
	ID            uint `gorm:"primaryKey"`
	Currency      string
	Cash          float64
	InitialCapital float64
}

type Position struct {
	ID         uint `gorm:"primaryKey"`
	AccountID  uint `gorm:"index"`
	Symbol     string `gorm:"index"`
	Qty        float64
	AvgCost    float64
	StopLoss   *float64
	TakeProfit *float64
}

type Order struct {
	ID         uint `gorm:"primaryKey"`
	AccountID  uint
	RunID      *uint
	ApprovalID *uint
	Symbol     string
	Side       string
	Qty        float64
	FillPrice  float64
	Notional   float64
	Status     string // filled
	TradeDate  string
}

type WatchlistSymbol struct {
	ID     uint `gorm:"primaryKey"`
	Symbol string `gorm:"uniqueIndex"`
}

type WorkflowRun struct {
	ID        uint `gorm:"primaryKey"`
	TradeDate string `gorm:"index"`
	Status    string `gorm:"index"`
	ErrorMsg  string
}

type WorkflowStepResult struct {
	ID     uint `gorm:"primaryKey"`
	RunID  uint `gorm:"index"`
	Step   string
	Status string
	PayloadJSON string `gorm:"type:text"`
}

type TradeProposal struct {
	ID uint `gorm:"primaryKey"`
	RunID uint `gorm:"index"`
	Symbol string
	Side string
	Qty float64
	TargetWeight *float64
	StopLoss *float64
	TakeProfit *float64
	EstimatedNotional float64
	EstimatedCashImpact float64
	Status string // pending_auto|awaiting_approval|filled|rejected|cancelled
}

type Approval struct {
	ID uint `gorm:"primaryKey"`
	ProposalID uint `gorm:"uniqueIndex"`
	Status string // pending|approved|rejected
	BreachReasonsJSON string `gorm:"type:text"`
	Note string
	DecidedBy *uint
}

type RiskRuleConfig struct {
	ID uint `gorm:"primaryKey"`
	Key string `gorm:"uniqueIndex"`
	ValueFloat float64
}

type NavSnapshot struct {
	ID uint `gorm:"primaryKey"`
	TradeDate string `gorm:"uniqueIndex"`
	Nav float64
	Cash float64
	Equity float64
}
```

- [ ] **Step 1: Table name smoke test** using sqlite in-memory AutoMigrate all models — expect no error

- [ ] **Step 2: Implement models**

- [ ] **Step 3: Test PASS + commit** `test: add gorm models with automigrate smoke`

---

### Task 02.3: DB connect + seed admin/account/watchlist/risk

**Files:**
- Create: `services/api/internal/db/db.go`
- Create: `services/api/internal/db/seed.go`
- Test: `services/api/internal/db/seed_test.go`

**Interfaces:**
- Produces: `db.Connect(dsn) (*gorm.DB, error)`, `db.AutoMigrate(db)`, `db.Seed(db, cfg)`
- Seed: one User, one Account with `InitialCash`, watchlist symbols, risk keys `max_order_notional`, `max_single_name_weight`, `min_cash_ratio`

- [ ] **Step 1: Failing test** — Seed on sqlite creates user `admin` and cash `100000`

- [ ] **Step 2: Implement Connect/AutoMigrate/Seed (bcrypt hash password)**

- [ ] **Step 3: PASS + commit** `feat: db migrate and bootstrap seed`

---

### Task 02.4: JWT auth handlers

**Files:**
- Create: `services/api/internal/auth/password.go`
- Create: `services/api/internal/auth/jwt.go`
- Create: `services/api/internal/auth/handlers.go`
- Create: `services/api/internal/auth/middleware.go`
- Test: `services/api/internal/auth/auth_test.go`
- Modify: `services/api/internal/httpserver/router.go`

**Interfaces:**
- Produces:
  - `POST /api/v1/auth/login`
  - `GET /api/v1/auth/me` (Bearer)
  - `MiddlewareAuth` setting `user_id` in Gin context

- [ ] **Step 1: Unit tests** — Hash/check password; issue/parse JWT; login success/fail with sqlite

- [ ] **Step 2: Implement**

- [ ] **Step 3: PASS + commit** `feat: jwt login and me endpoints`

---

### Task 02.5: Wire main with migrate+seed

**Files:**
- Modify: `services/api/cmd/api/main.go`

**Interfaces:**
- Produces: process that migrates, seeds, serves `:8080`

- [ ] **Step 1: Wire config → db → seed → router**

- [ ] **Step 2: Document local run in `services/api/README.md`** (docker postgres optional for later)

- [ ] **Step 3: Commit** `feat: api main wires db seed and auth routes`
