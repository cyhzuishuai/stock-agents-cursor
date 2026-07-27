package httpserver

import (
	"math"
	"net/http"
	"strings"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
)

type watchlistCreateRequest struct {
	Symbol  string `json:"symbol"`
	CanHold *bool  `json:"can_hold"`
}

type watchlistPatchRequest struct {
	CanHold *bool `json:"can_hold"`
}

type riskPatchRequest struct {
	Value *float64 `json:"value"`
}

func normalizeSymbol(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func (h *API) Settings(c *gin.Context) {
	var symbols []models.WatchlistSymbol
	if err := h.DB.WithContext(c.Request.Context()).Order("id ASC").Find(&symbols).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	watchlist := make([]gin.H, 0, len(symbols))
	for _, s := range symbols {
		watchlist = append(watchlist, gin.H{
			"symbol":   s.Symbol,
			"can_hold": s.CanHold,
		})
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

func (h *API) AddWatchlistSymbol(c *gin.Context) {
	var req watchlistCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	symbol := normalizeSymbol(req.Symbol)
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}
	canHold := true
	if req.CanHold != nil {
		canHold = *req.CanHold
	}
	row := models.WatchlistSymbol{Symbol: symbol, CanHold: canHold}
	err := h.DB.WithContext(c.Request.Context()).Create(&row).Error
	if err != nil {
		var existing models.WatchlistSymbol
		if h.DB.Where("symbol = ?", symbol).First(&existing).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "symbol already on watchlist"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"symbol": row.Symbol, "can_hold": row.CanHold})
}

func (h *API) PatchWatchlistSymbol(c *gin.Context) {
	symbol := normalizeSymbol(c.Param("symbol"))
	var req watchlistPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.CanHold == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can_hold required"})
		return
	}
	var row models.WatchlistSymbol
	if err := h.DB.WithContext(c.Request.Context()).Where("symbol = ?", symbol).First(&row).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found"})
		return
	}
	row.CanHold = *req.CanHold
	if err := h.DB.WithContext(c.Request.Context()).Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": row.Symbol, "can_hold": row.CanHold})
}

func (h *API) DeleteWatchlistSymbol(c *gin.Context) {
	symbol := normalizeSymbol(c.Param("symbol"))
	res := h.DB.WithContext(c.Request.Context()).Where("symbol = ?", symbol).Delete(&models.WatchlistSymbol{})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *API) PatchRiskRule(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	var req riskPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value required as number"})
		return
	}
	v := *req.Value
	if math.IsNaN(v) || math.IsInf(v, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value must be finite"})
		return
	}
	var row models.RiskRuleConfig
	if err := h.DB.WithContext(c.Request.Context()).Where("key = ?", key).First(&row).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "risk key not found"})
		return
	}
	row.ValueFloat = v
	if err := h.DB.WithContext(c.Request.Context()).Save(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": row.Key, "value": row.ValueFloat})
}
