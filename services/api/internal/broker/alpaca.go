package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cyh/stock-agents/services/api/internal/config"
)

// Alpaca is a thin REST client for Alpaca Paper Trading API.
type Alpaca struct {
	BaseURL string
	Key     string
	Secret  string
	HTTP    *http.Client
}

// NewAlpaca builds an Alpaca client from config. Returns an error if credentials are empty.
func NewAlpaca(cfg *config.Config) (*Alpaca, error) {
	if cfg == nil || strings.TrimSpace(cfg.AlpacaAPIKey) == "" || strings.TrimSpace(cfg.AlpacaAPISecret) == "" {
		return nil, fmt.Errorf("alpaca credentials required")
	}
	baseURL := strings.TrimSpace(cfg.AlpacaBaseURL)
	if baseURL == "" {
		baseURL = "https://paper-api.alpaca.markets"
	}
	return &Alpaca{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Key:     cfg.AlpacaAPIKey,
		Secret:  cfg.AlpacaAPISecret,
		HTTP:    http.DefaultClient,
	}, nil
}

func (c *Alpaca) GetAccount(ctx context.Context) (Account, error) {
	var raw alpacaAccount
	if err := c.doJSON(ctx, http.MethodGet, "/v2/account", nil, &raw); err != nil {
		return Account{}, err
	}
	return Account{
		ID:             raw.ID,
		Cash:           raw.Cash.Float64(),
		Equity:         raw.Equity.Float64(),
		BuyingPower:    raw.BuyingPower.Float64(),
		PortfolioValue: raw.PortfolioValue.Float64(),
	}, nil
}

func (c *Alpaca) ListPositions(ctx context.Context) ([]Position, error) {
	var raw []alpacaPosition
	if err := c.doJSON(ctx, http.MethodGet, "/v2/positions", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Position, 0, len(raw))
	for _, p := range raw {
		out = append(out, Position{
			Symbol:       p.Symbol,
			Qty:          p.Qty.Float64(),
			AvgCost:      p.AvgEntryPrice.Float64(),
			MarketValue:  p.MarketValue.Float64(),
			CurrentPrice: p.CurrentPrice.Float64(),
			UnrealizedPL: p.UnrealizedPL.Float64(),
		})
	}
	return out, nil
}

func (c *Alpaca) SubmitOrder(ctx context.Context, req OrderRequest) (Order, error) {
	side := strings.ToLower(strings.TrimSpace(req.Side))
	tif := req.TimeInForce
	if tif == "" {
		tif = "day"
	}
	typ := req.Type
	if typ == "" {
		typ = "market"
	}
	body := map[string]any{
		"symbol":          req.Symbol,
		"side":            side,
		"qty":             formatQty(req.Qty),
		"client_order_id": req.ClientOrderID,
		"time_in_force":   tif,
		"type":            typ,
	}
	var raw alpacaOrder
	if err := c.doJSON(ctx, http.MethodPost, "/v2/orders", body, &raw); err != nil {
		return Order{}, err
	}
	return mapOrder(raw), nil
}

func (c *Alpaca) GetOrder(ctx context.Context, brokerOrderID string) (Order, error) {
	var raw alpacaOrder
	path := "/v2/orders/" + url.PathEscape(brokerOrderID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return Order{}, err
	}
	return mapOrder(raw), nil
}

func (c *Alpaca) ListOrders(ctx context.Context, status string) ([]Order, error) {
	path := "/v2/orders"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var raw []alpacaOrder
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Order, 0, len(raw))
	for _, o := range raw {
		out = append(out, mapOrder(o))
	}
	return out, nil
}

func (c *Alpaca) doJSON(ctx context.Context, method, path string, body any, dest any) error {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", c.Key)
	req.Header.Set("APCA-API-SECRET-KEY", c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("alpaca %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if dest == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type alpacaAccount struct {
	ID             string      `json:"id"`
	Cash           flexFloat64 `json:"cash"`
	Equity         flexFloat64 `json:"equity"`
	BuyingPower    flexFloat64 `json:"buying_power"`
	PortfolioValue flexFloat64 `json:"portfolio_value"`
}

type alpacaPosition struct {
	Symbol        string      `json:"symbol"`
	Qty           flexFloat64 `json:"qty"`
	AvgEntryPrice flexFloat64 `json:"avg_entry_price"`
	MarketValue   flexFloat64 `json:"market_value"`
	CurrentPrice  flexFloat64 `json:"current_price"`
	UnrealizedPL  flexFloat64 `json:"unrealized_pl"`
}

type alpacaOrder struct {
	ID             string      `json:"id"`
	ClientOrderID  string      `json:"client_order_id"`
	Symbol         string      `json:"symbol"`
	Side           string      `json:"side"`
	Qty            flexFloat64 `json:"qty"`
	FilledQty      flexFloat64 `json:"filled_qty"`
	FilledAvgPrice flexFloat64 `json:"filled_avg_price"`
	Status         string      `json:"status"`
}

func mapOrder(o alpacaOrder) Order {
	return Order{
		ID:             o.ID,
		ClientOrderID:  o.ClientOrderID,
		Symbol:         o.Symbol,
		Side:           strings.ToLower(o.Side),
		Qty:            o.Qty.Float64(),
		FilledQty:      o.FilledQty.Float64(),
		FilledAvgPrice: o.FilledAvgPrice.Float64(),
		Status:         o.Status,
	}
}

func formatQty(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

// flexFloat64 accepts JSON numbers or numeric strings.
type flexFloat64 float64

func (f *flexFloat64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*f = 0
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*f = flexFloat64(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = flexFloat64(v)
	return nil
}

func (f flexFloat64) Float64() float64 { return float64(f) }
