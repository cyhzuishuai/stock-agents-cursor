package httpserver

import (
	"net/http"
	"strings"

	"github.com/cyh/stock-agents/services/api/internal/symbolsearch"
	"github.com/gin-gonic/gin"
)

// SearchSymbols proxies Alpaca asset search + snapshot quotes for Settings autocomplete.
func (h *API) SearchSymbols(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusOK, []symbolsearch.Result{})
		return
	}

	if h.SymbolSearch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alpaca not configured"})
		return
	}

	results, err := h.SymbolSearch.Search(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}
	if results == nil {
		results = []symbolsearch.Result{}
	}
	c.JSON(http.StatusOK, results)
}
