package risk

func (e Engine) Evaluate(state PortfolioState, p Proposal) Decision {
	var breaches []string

	if p.EstimatedNotional > e.MaxOrderNotional {
		breaches = append(breaches, "max_order_notional")
	}

	if p.EstimatedCashImpact < 0 && state.Equity > 0 {
		postCash := state.Cash + p.EstimatedCashImpact
		if postCash/state.Equity < e.MinCashRatio {
			breaches = append(breaches, "min_cash_ratio")
		}
	}

	return Decision{
		AutoExecute:   len(breaches) == 0,
		BreachReasons: breaches,
	}
}
