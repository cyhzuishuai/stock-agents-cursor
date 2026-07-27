package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (h *API) Portfolio(c *gin.Context) {
	if !h.requireBroker(c) {
		return
	}

	acct, err := h.Broker.GetAccount(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	positions, err := h.Broker.ListPositions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	localStops := map[string]models.Position{}
	var local []models.Position
	if err := h.DB.WithContext(c.Request.Context()).Find(&local).Error; err == nil {
		for _, p := range local {
			localStops[p.Symbol] = p
		}
	}

	nav := acct.Equity
	if nav == 0 && acct.PortfolioValue > 0 {
		nav = acct.PortfolioValue
	}

	out := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		price := p.CurrentPrice
		mv := p.MarketValue
		if mv == 0 && price > 0 {
			mv = p.Qty * price
		}
		weight := 0.0
		if nav > 0 {
			weight = mv / nav
		}
		var stopLoss, takeProfit any
		if loc, ok := localStops[p.Symbol]; ok {
			if loc.StopLoss != nil {
				stopLoss = *loc.StopLoss
			}
			if loc.TakeProfit != nil {
				takeProfit = *loc.TakeProfit
			}
		}
		out = append(out, gin.H{
			"symbol":         p.Symbol,
			"qty":            p.Qty,
			"avg_cost":       p.AvgCost,
			"stop_loss":      stopLoss,
			"take_profit":    takeProfit,
			"market_price":   price,
			"unrealized_pnl": p.UnrealizedPL,
			"weight":         weight,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"cash":      acct.Cash,
		"positions": out,
	})
}
