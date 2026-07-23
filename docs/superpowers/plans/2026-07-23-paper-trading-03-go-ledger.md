# Plan 03 — Go Paper Ledger

> **Wave:** 2  
> **Track:** T-GO-LEDGER  
> **Depends on:** Plan 02 (models + db)  
> **Parallel with:** Plan 04, Plan 05b, Plan 06×4, Plan 08 pages (mocked)

**Goal:** Pure ledger service that applies paper fills and upserts NAV — no HTTP yet (called by workflow later).

**Tech Stack:** Go, Gorm, testify.

## Global Constraints

Only edit `services/api/internal/ledger/**`. Fee rate default `0`. No shorting / no cash overdraft.

---

### Task 03.1: Ledger service skeleton + price map input

**Files:**
- Create: `services/api/internal/ledger/ledger.go`
- Create: `services/api/internal/ledger/types.go`
- Test: `services/api/internal/ledger/ledger_buy_test.go`

**Interfaces:**
- Produces:

```go
type FillRequest struct {
	AccountID  uint
	RunID      *uint
	ApprovalID *uint
	Symbol     string
	Side       string // buy|sell
	Qty        float64
	FillPrice  float64
	TradeDate  string
	StopLoss   *float64
	TakeProfit *float64
}

type Service struct{ DB *gorm.DB }

func (s *Service) ApplyFill(ctx context.Context, req FillRequest) (Order, error)
func (s *Service) UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (NavSnapshot, error)
```

- [ ] **Step 1: Failing test — buy 10 @ 100 with cash 100000 reduces cash by 1000 and creates position**

```go
func TestApplyFillBuy(t *testing.T) {
	// sqlite migrate Account{Cash:100000}, then ApplyFill buy AAPL qty 10 price 100
	// assert cash == 99000, position qty 10 avg 100, order status filled
}
```

- [ ] **Step 2: Implement ApplyFill buy path only**

- [ ] **Step 3: PASS + commit** `feat: ledger buy fill`

---

### Task 03.2: Sell, avg cost, insufficient cash/qty

**Files:**
- Modify: `services/api/internal/ledger/ledger.go`
- Test: `services/api/internal/ledger/ledger_sell_test.go`
- Test: `services/api/internal/ledger/ledger_errors_test.go`

**Interfaces:**
- Produces: errors `ErrInsufficientCash`, `ErrInsufficientQty`, `ErrInvalidSide`

- [ ] **Step 1: Tests**
  - Sell reduces qty and increases cash
  - Sell all deletes or zeros position (choose **delete row when qty==0**)
  - Buy with notional > cash → `ErrInsufficientCash`
  - Sell qty > position → `ErrInsufficientQty`
  - Second buy updates weighted avg_cost

- [ ] **Step 2: Implement**

- [ ] **Step 3: PASS + commit** `feat: ledger sell and invariant errors`

---

### Task 03.3: Stop/take persistence + UpsertNAV

**Files:**
- Modify: `services/api/internal/ledger/ledger.go`
- Test: `services/api/internal/ledger/nav_test.go`

**Interfaces:**
- `ApplyFill` writes StopLoss/TakeProfit onto position when provided
- `UpsertNAV`: `nav = cash + sum(qty*mark[symbol])`; missing mark → error `ErrMissingMark`

- [ ] **Step 1: Failing NAV tests**

- [ ] **Step 2: Implement UpsertNAV (unique on trade_date)**

- [ ] **Step 3: PASS + commit** `feat: ledger nav snapshot upsert`

---

### Task 03.4: Account snapshot builder

**Files:**
- Create: `services/api/internal/ledger/snapshot.go`
- Test: `services/api/internal/ledger/snapshot_test.go`

**Interfaces:**
- Produces JSON-serializable snapshot matching `agent_run_request.account_snapshot`:

```go
func (s *Service) AccountSnapshot(ctx context.Context, accountID uint) (AccountSnapshot, error)
```

- [ ] **Step 1: Test snapshot shape**

- [ ] **Step 2: Implement**

- [ ] **Step 3: Commit** `feat: ledger account snapshot for agents`
