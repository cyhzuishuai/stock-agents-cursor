# Plan 04 — Go Deterministic Risk Engine

> **Wave:** 2  
> **Track:** T-GO-RISK  
> **Depends on:** Plan 02 models (RiskRuleConfig)  
> **Parallel with:** Plan 03, 05b, 06, 08

**Goal:** Pure function/service evaluating trade proposals against thresholds; authoritative gate.

**Tech Stack:** Go unit tests only (no HTTP).

---

### Task 04.1: Types + load rules from DB/config overlay

**Files:**
- Create: `services/api/internal/risk/types.go`
- Create: `services/api/internal/risk/rules.go`
- Test: `services/api/internal/risk/rules_load_test.go`

**Interfaces:**

```go
type Proposal struct {
	Symbol string
	Side string
	Qty float64
	EstimatedNotional float64
	EstimatedCashImpact float64
	FillPrice float64 // expected EOD price for weight math
}

type PortfolioState struct {
	Cash float64
	Equity float64 // cash + mtm
	Positions map[string]float64 // symbol -> qty
	Marks map[string]float64     // symbol -> price
	PeakNav float64              // for drawdown optional
}

type Decision struct {
	AutoExecute bool
	BreachReasons []string
}

type Engine struct {
	MaxOrderNotional float64
	MaxSingleNameWeight float64
	MinCashRatio float64
	MaxTopConcentration float64 // optional; default 0 = disabled
	MaxDrawdown float64         // optional; default 0 = disabled
}

func LoadEngineFromMap(m map[string]float64) Engine
func (e Engine) Evaluate(state PortfolioState, p Proposal) Decision
```

- [ ] **Step 1: Test LoadEngineFromMap defaults**

- [ ] **Step 2: Implement LoadEngineFromMap**

- [ ] **Step 3: Commit** `feat: risk engine config load`

---

### Task 04.2: Max notional + min cash ratio rules

**Files:**
- Modify: `services/api/internal/risk/evaluate.go` (or `rules.go`)
- Test: `services/api/internal/risk/evaluate_notional_cash_test.go`

- [ ] **Step 1: Tests**
  - notional `10001` with max `10000` → breach `max_order_notional`
  - buy that would leave cash ratio `< 0.10` → breach `min_cash_ratio`
  - sell increasing cash always passes cash ratio

- [ ] **Step 2: Implement**

- [ ] **Step 3: Commit** `feat: risk notional and cash ratio gates`

---

### Task 04.3: Single-name weight + optional concentration

**Files:**
- Modify: evaluate
- Test: `services/api/internal/risk/evaluate_weight_test.go`

- [ ] **Step 1: Tests**
  - After buy, symbol weight `> 0.20` → `max_single_name_weight`
  - When `MaxTopConcentration` set, top-1 weight breach → `max_concentration`

- [ ] **Step 2: Implement post-trade simulated weights using Marks**

- [ ] **Step 3: Commit** `feat: risk weight and concentration gates`

---

### Task 04.4: Advisory merge helper (non-binding)

**Files:**
- Create: `services/api/internal/risk/advisory.go`
- Test: `services/api/internal/risk/advisory_test.go`

**Interfaces:**

```go
// MergeAdvisory only attaches audit info; never flips AutoExecute true→false unless rules already false.
func Annotate(d Decision, suggestedAction string) Decision
```

- [ ] **Step 1: Test that suggested `review` does not force breach if rules pass**

- [ ] **Step 2: Implement no-op annotate storing nothing in Decision (workflow stores advisory separately) — keep function documenting policy**

- [ ] **Step 3: Commit** `test: document advisory non-binding policy`
