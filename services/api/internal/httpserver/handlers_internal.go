package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *API) InternalEOD(c *gin.Context) {
	expected := ""
	if h.Config != nil {
		expected = h.Config.InternalEODToken
	}
	if expected == "" || c.GetHeader("X-Internal-Token") != expected {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req eodRequest
	_ = c.ShouldBindJSON(&req)
	tradeDate := req.TradeDate
	if tradeDate == "" {
		tradeDate = defaultTradeDate()
	}
	force := req.Force || queryForce(c)
	h.triggerEOD(c, tradeDate, force)
}
