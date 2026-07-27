package risk

import "testing"

func TestAnnotateReviewDoesNotForceBreachWhenRulesPass(t *testing.T) {
	e := LoadEngineFromMap(nil)
	state := PortfolioState{
		Cash:      90000,
		Equity:    100000,
		Positions: map[string]float64{"AAPL": 50},
		Marks:     map[string]float64{"AAPL": 200},
	}
	p := Proposal{
		Symbol:              "AAPL",
		Side:                "buy",
		Qty:                 5,
		EstimatedNotional:   1000,
		EstimatedCashImpact: -1000,
		FillPrice:           200,
	}

	d := e.Evaluate(state, p)
	if !d.AutoExecute {
		t.Fatalf("precondition: rules must pass, got BreachReasons=%v", d.BreachReasons)
	}

	got := Annotate(d, "review")
	if !got.AutoExecute {
		t.Fatal("advisory review must not flip AutoExecute when rules pass")
	}
	if len(got.BreachReasons) != 0 {
		t.Fatalf("advisory review must not add breach reasons, got %v", got.BreachReasons)
	}
}

func TestAnnotateDoesNotRestoreAutoExecuteWhenRulesFail(t *testing.T) {
	d := Decision{AutoExecute: false, BreachReasons: []string{"max_order_notional"}}
	got := Annotate(d, "auto")
	if got.AutoExecute {
		t.Fatal("advisory auto must not override rule breach")
	}
	if len(got.BreachReasons) != 1 || got.BreachReasons[0] != "max_order_notional" {
		t.Fatalf("BreachReasons: got %v want [max_order_notional]", got.BreachReasons)
	}
}
