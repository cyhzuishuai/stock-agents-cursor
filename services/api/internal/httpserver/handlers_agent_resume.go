package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type agentResumeBody struct {
	Agent         string          `json:"agent"`
	HumanResponse json.RawMessage `json:"human_response"`
}

func (h *API) PostAgentResume(c *gin.Context) {
	if h.Runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workflow runner not configured"})
		return
	}
	runID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	var req agentResumeBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.Agent == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent is required"})
		return
	}
	if len(req.HumanResponse) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "human_response is required"})
		return
	}

	err = h.Runner.ResumeAgent(c.Request.Context(), uint(runID), req.Agent, req.HumanResponse)
	if err != nil {
		if errors.Is(err, workflow.ErrRunNotAwaitingAgentInput) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run_id": runID, "status": "resumed"})
}
