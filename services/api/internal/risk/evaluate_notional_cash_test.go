package risk

import "testing"

func TestEvaluateMaxOrderNotionalBreach(t *testing.T) {
	e := LoadEngineFromMap(nil)
	state := PortfolioState{Cash: 50000, Equity: 100000}
	p := Proposal{EstimatedNotional: 10001, EstimatedCashImpact: -10001}

	d := e.Evaluate(state, p)
	if d.AutoExecute {
		t.Fatal("expected AutoExecute false for notional breach")
	}
	if len(d.BreachReasons) != 1 || d.BreachReasons[0] != "max_order_notional" {
		t.Fatalf("BreachReasons: got %v want [max_order_notional]", d.BreachReasons)
	}
}

func TestEvaluateMinCashRatioBreachOnBuy(t *testing.T) {
	e := LoadEngineFromMap(nil)
	state := PortfolioState{Cash: 5000, Equity: 10000}
	p := Proposal{Side: "buy", EstimatedNotional: 4500, EstimatedCashImpact: -4500}

	d := e.Evaluate(state, p)
	if d.AutoExecute {
		t.Fatal("expected AutoExecute false for min cash ratio breach")
	}
	if len(d.BreachReasons) != 1 || d.BreachReasons[0] != "min_cash_ratio" {
		t.Fatalf("BreachReasons: got %v want [min_cash_ratio]", d.BreachReasons)
	}
}

func TestEvaluateSellPassesCashRatio(t *testing.T) {
	e := LoadEngineFromMap(nil)
	state := PortfolioState{Cash: 500, Equity: 10000}
	p := Proposal{Side: "sell", EstimatedNotional: 1000, EstimatedCashImpact: 1000}

	d := e.Evaluate(state, p)
	if !d.AutoExecute {
		t.Fatalf("expected AutoExecute true for sell, got breaches %v", d.BreachReasons)
	}
	if len(d.BreachReasons) != 0 {
		t.Fatalf("BreachReasons: got %v want none", d.BreachReasons)
	}
}
