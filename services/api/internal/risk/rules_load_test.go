package risk_test

import (
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/risk"
)

func TestLoadEngineFromMapDefaults(t *testing.T) {
	for _, m := range []map[string]float64{nil, {}} {
		e := risk.LoadEngineFromMap(m)
		if e.MaxOrderNotional != 10000 {
			t.Fatalf("MaxOrderNotional: got %v want 10000", e.MaxOrderNotional)
		}
		if e.MaxSingleNameWeight != 0.20 {
			t.Fatalf("MaxSingleNameWeight: got %v want 0.20", e.MaxSingleNameWeight)
		}
		if e.MinCashRatio != 0.10 {
			t.Fatalf("MinCashRatio: got %v want 0.10", e.MinCashRatio)
		}
		if e.MaxTopConcentration != 0 {
			t.Fatalf("MaxTopConcentration: got %v want 0", e.MaxTopConcentration)
		}
		if e.MaxDrawdown != 0 {
			t.Fatalf("MaxDrawdown: got %v want 0", e.MaxDrawdown)
		}
	}
}

func TestLoadEngineFromMapOverrides(t *testing.T) {
	e := risk.LoadEngineFromMap(map[string]float64{
		"max_order_notional":     5000,
		"max_single_name_weight": 0.15,
		"min_cash_ratio":         0.05,
	})
	if e.MaxOrderNotional != 5000 {
		t.Fatalf("MaxOrderNotional: got %v want 5000", e.MaxOrderNotional)
	}
	if e.MaxSingleNameWeight != 0.15 {
		t.Fatalf("MaxSingleNameWeight: got %v want 0.15", e.MaxSingleNameWeight)
	}
	if e.MinCashRatio != 0.05 {
		t.Fatalf("MinCashRatio: got %v want 0.05", e.MinCashRatio)
	}
}

func TestLoadEngineFromMapPartialUsesDefaults(t *testing.T) {
	e := risk.LoadEngineFromMap(map[string]float64{
		"max_order_notional": 8000,
	})
	if e.MaxOrderNotional != 8000 {
		t.Fatalf("MaxOrderNotional: got %v want 8000", e.MaxOrderNotional)
	}
	if e.MaxSingleNameWeight != 0.20 {
		t.Fatalf("MaxSingleNameWeight: got %v want default 0.20", e.MaxSingleNameWeight)
	}
	if e.MinCashRatio != 0.10 {
		t.Fatalf("MinCashRatio: got %v want default 0.10", e.MinCashRatio)
	}
}

func TestLoadEngineFromMapOptionalKeys(t *testing.T) {
	e := risk.LoadEngineFromMap(map[string]float64{
		"max_top_concentration": 0.40,
		"max_drawdown":          0.10,
	})
	if e.MaxTopConcentration != 0.40 {
		t.Fatalf("MaxTopConcentration: got %v want 0.40", e.MaxTopConcentration)
	}
	if e.MaxDrawdown != 0.10 {
		t.Fatalf("MaxDrawdown: got %v want 0.10", e.MaxDrawdown)
	}
}
