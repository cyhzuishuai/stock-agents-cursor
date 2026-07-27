package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (h *API) Settings(c *gin.Context) {
	var symbols []models.WatchlistSymbol
	if err := h.DB.WithContext(c.Request.Context()).Order("id ASC").Find(&symbols).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	watchlist := make([]string, 0, len(symbols))
	for _, s := range symbols {
		watchlist = append(watchlist, s.Symbol)
	}

	var rules []models.RiskRuleConfig
	if err := h.DB.WithContext(c.Request.Context()).Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	riskRules := map[string]any{}
	for _, r := range rules {
		riskRules[r.Key] = r.ValueFloat
	}

	provider := ""
	if h.Config != nil {
		provider = h.Config.MarketDataProvider
	}

	c.JSON(http.StatusOK, gin.H{
		"watchlist":             watchlist,
		"risk_rules":            riskRules,
		"market_data_provider":  provider,
	})
}
