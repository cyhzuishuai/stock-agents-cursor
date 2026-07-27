package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (h *API) Portfolio(c *gin.Context) {
	account, err := h.loadAccount(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	marks := h.latestMarks(c.Request.Context())
	var positions []models.Position
	if err := h.DB.WithContext(c.Request.Context()).Where("account_id = ?", account.ID).Find(&positions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var equity float64
	out := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		price := markOrCost(marks, p)
		mv := p.Qty * price
		equity += mv
		unrealized := mv - p.Qty*p.AvgCost
		out = append(out, gin.H{
			"symbol":         p.Symbol,
			"qty":            p.Qty,
			"avg_cost":       p.AvgCost,
			"stop_loss":      p.StopLoss,
			"take_profit":    p.TakeProfit,
			"market_price":   price,
			"unrealized_pnl": unrealized,
			"weight":         0.0,
		})
	}
	nav := account.Cash + equity
	if nav > 0 {
		for i := range out {
			price := out[i]["market_price"].(float64)
			qty := out[i]["qty"].(float64)
			out[i]["weight"] = (qty * price) / nav
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"cash":      account.Cash,
		"positions": out,
	})
}
