package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *API) ListRuns(c *gin.Context) {
	var runs []models.WorkflowRun
	if err := h.DB.WithContext(c.Request.Context()).Order("id DESC").Find(&runs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(runs))
	for _, r := range runs {
		out = append(out, gin.H{
			"id":         r.ID,
			"trade_date": r.TradeDate,
			"status":     r.Status,
			"created_at": createdAtPlaceholder(r.ID),
		})
	}
	c.JSON(http.StatusOK, out)
}

func (h *API) GetRun(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}

	var run models.WorkflowRun
	if err := h.DB.WithContext(c.Request.Context()).First(&run, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var steps []models.WorkflowStepResult
	if err := h.DB.WithContext(c.Request.Context()).Where("run_id = ?", run.ID).Order("id ASC").Find(&steps).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var proposals []models.TradeProposal
	if err := h.DB.WithContext(c.Request.Context()).Where("run_id = ?", run.ID).Order("id ASC").Find(&proposals).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var orders []models.Order
	if err := h.DB.WithContext(c.Request.Context()).Where("run_id = ?", run.ID).Order("id ASC").Find(&orders).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         run.ID,
		"trade_date": run.TradeDate,
		"status":     run.Status,
		"steps":      steps,
		"proposals":  proposals,
		"orders":     orders,
	})
}

type eodRequest struct {
	TradeDate string `json:"trade_date"`
}

func (h *API) PostEOD(c *gin.Context) {
	var req eodRequest
	_ = c.ShouldBindJSON(&req)
	tradeDate := req.TradeDate
	if tradeDate == "" {
		tradeDate = defaultTradeDate()
	}
	h.triggerEOD(c, tradeDate)
}

func (h *API) ListApprovals(c *gin.Context) {
	status := c.Query("status")
	q := h.DB.WithContext(c.Request.Context()).Model(&models.Approval{})
	if status != "" {
		q = q.Where("status = ?", status)
	}

	var rows []models.Approval
	if err := q.Order("id DESC").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	out := make([]gin.H, 0, len(rows))
	for _, a := range rows {
		var proposal models.TradeProposal
		if err := h.DB.WithContext(c.Request.Context()).First(&proposal, a.ProposalID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var reasons []string
		if a.BreachReasonsJSON != "" {
			_ = json.Unmarshal([]byte(a.BreachReasonsJSON), &reasons)
		}
		if reasons == nil {
			reasons = []string{}
		}
		out = append(out, gin.H{
			"id":             a.ID,
			"proposal_id":    a.ProposalID,
			"symbol":         proposal.Symbol,
			"side":           proposal.Side,
			"qty":            proposal.Qty,
			"breach_reasons": reasons,
			"created_at":     createdAtPlaceholder(a.ID),
		})
	}
	c.JSON(http.StatusOK, out)
}

func defaultTradeDate() string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.Now().UTC().Format("2006-01-02")
	}
	return time.Now().In(loc).Format("2006-01-02")
}

func createdAtPlaceholder(_ uint) string {
	// Models lack CreatedAt; keep DTO key present for clients.
	return ""
}
