package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const symbolSearchMaxResults = 10

const yahooSearchURL = "https://query1.finance.yahoo.com/v1/finance/search"

// SymbolSearchResult is one symbol match for watchlist autocomplete.
type SymbolSearchResult struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type yahooSearchResponse struct {
	Quotes []yahooQuote `json:"quotes"`
}

type yahooQuote struct {
	Symbol    string `json:"symbol"`
	ShortName string `json:"shortname"`
	LongName  string `json:"longname"`
	QuoteType string `json:"quoteType"`
}

func (h *API) SearchSymbols(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusOK, []SymbolSearchResult{})
		return
	}

	client := h.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	u, err := url.Parse(yahooSearchURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}
	params := u.Query()
	params.Set("q", q)
	params.Set("quotesCount", fmt.Sprintf("%d", symbolSearchMaxResults))
	params.Set("newsCount", "0")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}

	var payload yahooSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "symbol search unavailable"})
		return
	}

	results := mapYahooQuotes(payload.Quotes)
	c.JSON(http.StatusOK, results)
}

func mapYahooQuotes(quotes []yahooQuote) []SymbolSearchResult {
	hasEquity := false
	for _, q := range quotes {
		if q.Symbol != "" && q.QuoteType == "EQUITY" {
			hasEquity = true
			break
		}
	}

	out := make([]SymbolSearchResult, 0, symbolSearchMaxResults)
	for _, q := range quotes {
		if q.Symbol == "" {
			continue
		}
		if hasEquity {
			if q.QuoteType != "EQUITY" {
				continue
			}
		} else if q.QuoteType != "" && q.QuoteType != "EQUITY" {
			continue
		}
		name := q.ShortName
		if name == "" {
			name = q.LongName
		}
		out = append(out, SymbolSearchResult{Symbol: q.Symbol, Name: name})
		if len(out) >= symbolSearchMaxResults {
			break
		}
	}
	return out
}
