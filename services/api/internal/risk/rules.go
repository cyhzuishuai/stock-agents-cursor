package risk

const (
	defaultMaxOrderNotional    = 10000
	defaultMaxSingleNameWeight = 0.20
	defaultMinCashRatio        = 0.10
)

func LoadEngineFromMap(m map[string]float64) Engine {
	e := Engine{
		MaxOrderNotional:    defaultMaxOrderNotional,
		MaxSingleNameWeight: defaultMaxSingleNameWeight,
		MinCashRatio:        defaultMinCashRatio,
		MaxTopConcentration: 0,
		MaxDrawdown:         0,
	}
	if m == nil {
		return e
	}
	if v, ok := m["max_order_notional"]; ok {
		e.MaxOrderNotional = v
	}
	if v, ok := m["max_single_name_weight"]; ok {
		e.MaxSingleNameWeight = v
	}
	if v, ok := m["min_cash_ratio"]; ok {
		e.MinCashRatio = v
	}
	if v, ok := m["max_top_concentration"]; ok {
		e.MaxTopConcentration = v
	}
	if v, ok := m["max_drawdown"]; ok {
		e.MaxDrawdown = v
	}
	return e
}

func (e Engine) Evaluate(_ PortfolioState, _ Proposal) Decision {
	return Decision{AutoExecute: true}
}
