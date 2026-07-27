package risk

type Proposal struct {
	Symbol              string
	Side                string
	Qty                 float64
	EstimatedNotional   float64
	EstimatedCashImpact float64
	FillPrice           float64 // expected EOD price for weight math
}

type PortfolioState struct {
	Cash      float64
	Equity    float64            // cash + mtm
	Positions map[string]float64 // symbol -> qty
	Marks     map[string]float64 // symbol -> price
	PeakNav   float64            // for drawdown optional
}

type Decision struct {
	AutoExecute   bool
	BreachReasons []string
}

type Engine struct {
	MaxOrderNotional    float64
	MaxSingleNameWeight float64
	MinCashRatio        float64
	MaxTopConcentration float64 // optional; default 0 = disabled
	MaxDrawdown         float64 // optional; default 0 = disabled
}
