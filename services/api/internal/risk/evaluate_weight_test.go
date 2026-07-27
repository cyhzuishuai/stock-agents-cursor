package risk

import "testing"

func TestEvaluateMaxSingleNameWeightBreachOnBuy(t *testing.T) {
	e := LoadEngineFromMap(nil)
	state := PortfolioState{
		Cash:      80000,
		Equity:    100000,
		Positions: map[string]float64{"AAPL": 100},
		Marks:     map[string]float64{"AAPL": 200},
	}
	p := Proposal{
		Symbol:              "AAPL",
		Side:                "buy",
		Qty:                 10,
		EstimatedNotional:   2000,
		EstimatedCashImpact: -2000,
		FillPrice:           200,
	}

	d := e.Evaluate(state, p)
	if d.AutoExecute {
		t.Fatal("expected AutoExecute false for single-name weight breach")
	}
	if len(d.BreachReasons) != 1 || d.BreachReasons[0] != "max_single_name_weight" {
		t.Fatalf("BreachReasons: got %v want [max_single_name_weight]", d.BreachReasons)
	}
}

func TestEvaluateMaxTopConcentrationBreach(t *testing.T) {
	e := LoadEngineFromMap(map[string]float64{
		"max_single_name_weight": 0.50,
		"max_top_concentration":  0.25,
	})
	state := PortfolioState{
		Cash:      72000,
		Equity:    100000,
		Positions: map[string]float64{"AAPL": 100, "MSFT": 50},
		Marks:     map[string]float64{"AAPL": 200, "MSFT": 160},
	}
	p := Proposal{
		Symbol:              "AAPL",
		Side:                "buy",
		Qty:                 30,
		EstimatedNotional:   6000,
		EstimatedCashImpact: -6000,
		FillPrice:           200,
	}

	d := e.Evaluate(state, p)
	if d.AutoExecute {
		t.Fatal("expected AutoExecute false for concentration breach")
	}
	if len(d.BreachReasons) != 1 || d.BreachReasons[0] != "max_concentration" {
		t.Fatalf("BreachReasons: got %v want [max_concentration]", d.BreachReasons)
	}
}

func TestEvaluateBuyWithinWeightLimits(t *testing.T) {
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
		Qty:                 10,
		EstimatedNotional:   2000,
		EstimatedCashImpact: -2000,
		FillPrice:           200,
	}

	d := e.Evaluate(state, p)
	if !d.AutoExecute {
		t.Fatalf("expected AutoExecute true, got breaches %v", d.BreachReasons)
	}
}
