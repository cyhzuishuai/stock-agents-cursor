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

	postPositions, postCash := simulatePostTrade(state, p)
	postEquity := postTradeEquity(postCash, postPositions, state.Marks, p.Symbol, p.FillPrice)
	if postEquity > 0 {
		weights := positionWeights(postPositions, postEquity, state.Marks, p.Symbol, p.FillPrice)
		if p.Side == "buy" {
			if w := weights[p.Symbol]; w > e.MaxSingleNameWeight {
				breaches = append(breaches, "max_single_name_weight")
			}
		}
		if e.MaxTopConcentration > 0 {
			if topWeight(weights) > e.MaxTopConcentration {
				breaches = append(breaches, "max_concentration")
			}
		}
	}

	return Decision{
		AutoExecute:   len(breaches) == 0,
		BreachReasons: breaches,
	}
}

func simulatePostTrade(state PortfolioState, p Proposal) (map[string]float64, float64) {
	positions := make(map[string]float64, len(state.Positions)+1)
	for sym, qty := range state.Positions {
		positions[sym] = qty
	}
	switch p.Side {
	case "buy":
		positions[p.Symbol] += p.Qty
	case "sell":
		positions[p.Symbol] -= p.Qty
	}
	return positions, state.Cash + p.EstimatedCashImpact
}

func markPrice(marks map[string]float64, symbol, tradedSymbol string, fillPrice float64) float64 {
	if symbol == tradedSymbol && fillPrice > 0 {
		return fillPrice
	}
	if marks != nil {
		if price, ok := marks[symbol]; ok {
			return price
		}
	}
	return fillPrice
}

func postTradeEquity(cash float64, positions map[string]float64, marks map[string]float64, tradedSymbol string, fillPrice float64) float64 {
	equity := cash
	for sym, qty := range positions {
		if qty == 0 {
			continue
		}
		equity += qty * markPrice(marks, sym, tradedSymbol, fillPrice)
	}
	return equity
}

func positionWeights(positions map[string]float64, equity float64, marks map[string]float64, tradedSymbol string, fillPrice float64) map[string]float64 {
	weights := make(map[string]float64, len(positions))
	for sym, qty := range positions {
		if qty == 0 {
			continue
		}
		weights[sym] = qty * markPrice(marks, sym, tradedSymbol, fillPrice) / equity
	}
	return weights
}

func topWeight(weights map[string]float64) float64 {
	var max float64
	for _, w := range weights {
		if w > max {
			max = w
		}
	}
	return max
}
