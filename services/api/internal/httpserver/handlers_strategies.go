package httpserver

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/gin-gonic/gin"
)

type strategyRequest struct {
	Name                 string `json:"name"`
	Description          string `json:"description"`
	PreOpenMinutes       int    `json:"pre_open_minutes"`
	IntradayEveryMinutes int    `json:"intraday_every_minutes"`
	IntradayStartET      string `json:"intraday_start_et"`
	IntradayEndET        string `json:"intraday_end_et"`
	ExecutionMode        string `json:"execution_mode"`
}

func (h *API) ListStrategies(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	strategies, err := h.Strategies.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, strategies)
}

func (h *API) GetStrategy(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	id, err := parseStrategyID(c)
	if err != nil {
		return
	}
	st, err := h.Strategies.Get(c.Request.Context(), id)
	if writeStrategyError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *API) CreateStrategy(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	var req strategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := h.Strategies.Create(c.Request.Context(), strategyInputFromRequest(req))
	if writeStrategyError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, st)
}

func (h *API) PatchStrategy(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	id, err := parseStrategyID(c)
	if err != nil {
		return
	}
	var req strategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := h.Strategies.Update(c.Request.Context(), id, strategyUpdateFromRequest(req))
	if writeStrategyError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if st.IsActive {
		if err := h.reloadScheduler(c); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, st)
}

func (h *API) ActivateStrategy(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	id, err := parseStrategyID(c)
	if err != nil {
		return
	}
	st, err := h.Strategies.Activate(c.Request.Context(), id)
	if writeStrategyError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.reloadScheduler(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *API) DeleteStrategy(c *gin.Context) {
	if h.Strategies == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "strategies service not configured"})
		return
	}
	id, err := parseStrategyID(c)
	if err != nil {
		return
	}
	err = h.Strategies.Delete(c.Request.Context(), id)
	if writeStrategyError(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func parseStrategyID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy id"})
		return 0, err
	}
	return uint(id), nil
}

func writeStrategyError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, strategy.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, strategy.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, strategy.ErrValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		return false
	}
	return true
}

func strategyInputFromRequest(req strategyRequest) strategy.CreateInput {
	return strategy.CreateInput{
		Name:                 req.Name,
		Description:          req.Description,
		PreOpenMinutes:       req.PreOpenMinutes,
		IntradayEveryMinutes: req.IntradayEveryMinutes,
		IntradayStartET:      req.IntradayStartET,
		IntradayEndET:        req.IntradayEndET,
		ExecutionMode:        req.ExecutionMode,
	}
}

func strategyUpdateFromRequest(req strategyRequest) strategy.UpdateInput {
	return strategy.UpdateInput{
		Name:                 req.Name,
		Description:          req.Description,
		PreOpenMinutes:       req.PreOpenMinutes,
		IntradayEveryMinutes: req.IntradayEveryMinutes,
		IntradayStartET:      req.IntradayStartET,
		IntradayEndET:        req.IntradayEndET,
		ExecutionMode:        req.ExecutionMode,
	}
}

func (h *API) reloadScheduler(c *gin.Context) error {
	if h.Scheduler == nil {
		return nil
	}
	return h.Scheduler.Reload(c.Request.Context())
}
