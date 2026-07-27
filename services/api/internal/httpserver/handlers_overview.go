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
	summary := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		price := markOrCost(marks, p)
		mv := p.Qty * price
		equity += mv
		summary = append(summary, gin.H{
			"symbol":       p.Symbol,
			"qty":          p.Qty,
			"market_value": mv,
			"weight":       0.0,
		})
	}
	nav := account.Cash + equity
	if nav > 0 {
		for i := range summary {
			mv := summary[i]["market_value"].(float64)
			summary[i]["weight"] = mv / nav
		}
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

	// Prefer persisted NAV snapshot when available for cash/equity/nav display.
	cash, equityOut, navOut := account.Cash, equity, nav
	if len(snaps) > 0 {
		last := snaps[len(snaps)-1]
		cash, equityOut, navOut = last.Cash, last.Equity, last.Nav
	}

	c.JSON(http.StatusOK, gin.H{
		"cash":                    cash,
		"equity":                  equityOut,
		"nav":                     navOut,
		"pending_approvals_count": pendingCount,
		"latest_run":              latestRun,
		"positions_summary":       summary,
		"nav_series":              navSeries,
	})
}
