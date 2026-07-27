package approvals

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handlers struct {
	Service   *Service
	JWTSecret string
}

type decideRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h *Handlers) Decide(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid approval id"})
		return
	}

	uidVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userID, ok := uidVal.(uint)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req decideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.Service.Decide(c.Request.Context(), uint(id), req.Decision, req.Note, userID); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrApprovalNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrApprovalNotPending), errors.Is(err, ErrInvalidDecision):
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handlers) CancelRun(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}

	if err := h.Service.CancelRun(c.Request.Context(), uint(id)); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrRunNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrRunNotCancellable):
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
