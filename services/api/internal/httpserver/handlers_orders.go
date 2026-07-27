package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/gin-gonic/gin"
)

func (h *API) ListOrders(c *gin.Context) {
	if !h.requireBroker(c) {
		return
	}

	brokerOrders, err := h.Broker.ListOrders(c.Request.Context(), "all")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var mirrors []models.Order
	_ = h.DB.WithContext(c.Request.Context()).Find(&mirrors).Error
	byBrokerID := map[string]models.Order{}
	byClientID := map[string]models.Order{}
	for _, m := range mirrors {
		if m.BrokerOrderID != "" {
			byBrokerID[m.BrokerOrderID] = m
		}
		if m.ClientOrderID != "" {
			byClientID[m.ClientOrderID] = m
		}
	}

	out := make([]gin.H, 0, len(brokerOrders))
	for _, o := range brokerOrders {
		item := gin.H{
			"broker_order_id":  o.ID,
			"client_order_id":  o.ClientOrderID,
			"symbol":           o.Symbol,
			"side":             o.Side,
			"qty":              o.Qty,
			"filled_qty":       o.FilledQty,
			"filled_avg_price": o.FilledAvgPrice,
			"status":           o.Status,
		}
		if m, ok := byBrokerID[o.ID]; ok && m.ProposalID != nil {
			item["proposal_id"] = *m.ProposalID
		} else if m, ok := byClientID[o.ClientOrderID]; ok && m.ProposalID != nil {
			item["proposal_id"] = *m.ProposalID
		}
		out = append(out, item)
	}

	c.JSON(http.StatusOK, gin.H{"orders": out})
}
