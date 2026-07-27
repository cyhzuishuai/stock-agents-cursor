package httpserver

import (
	"errors"
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *API) Overview(c *gin.Context) {
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

	cash := acct.Cash
	nav := acct.Equity
	if nav == 0 && acct.PortfolioValue > 0 {
		nav = acct.PortfolioValue
	}
	equity := nav - cash
	if equity < 0 {
		equity = 0
	}

	summary := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		mv := p.MarketValue
		if mv == 0 && p.CurrentPrice > 0 {
			mv = p.Qty * p.CurrentPrice
		}
		weight := 0.0
		if nav > 0 {
			weight = mv / nav
		}
		summary = append(summary, gin.H{
			"symbol":       p.Symbol,
			"qty":          p.Qty,
			"market_value": mv,
			"weight":       weight,
		})
	}

	var pendingCount int64
	_ = h.DB.WithContext(c.Request.Context()).Model(&models.Approval{}).
		Where("status = ?", workflow.ApprovalPending).Count(&pendingCount)

	var latest models.WorkflowRun
	var latestRun any
	err = h.DB.WithContext(c.Request.Context()).Order("id DESC").First(&latest).Error
	switch {
	case err == nil:
		latestRun = gin.H{"id": latest.ID, "trade_date": latest.TradeDate, "status": latest.Status}
	case errors.Is(err, gorm.ErrRecordNotFound):
		latestRun = nil
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var snaps []models.NavSnapshot
	if err := h.DB.WithContext(c.Request.Context()).Order("trade_date ASC").Find(&snaps).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	navSeries := make([]gin.H, 0, len(snaps))
	for _, s := range snaps {
		navSeries = append(navSeries, gin.H{"trade_date": s.TradeDate, "nav": s.Nav})
	}

	c.JSON(http.StatusOK, gin.H{
		"cash":                    cash,
		"equity":                  equity,
		"nav":                     nav,
		"pending_approvals_count": pendingCount,
		"latest_run":              latestRun,
		"positions_summary":       summary,
		"nav_series":              navSeries,
	})
}
