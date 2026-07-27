package symbolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/config"
)

const (
	defaultTTL   = time.Hour
	maxResults   = 10
	assetsPath   = "/v2/assets?status=active&asset_class=us_equity"
	snapshotsPath = "/v2/stocks/snapshots"
)

// Result is one autocomplete hit with optional quote fields.
type Result struct {
	Symbol     string   `json:"symbol"`
	Name       string   `json:"name"`
	Price      *float64 `json:"price"`
	Change     *float64 `json:"change"`
	ChangePct  *float64 `json:"change_pct"`
	AssetClass string   `json:"asset_class,omitempty"`
}

// Asset is a cached Alpaca us_equity row.
type Asset struct {
	Symbol string
	Name   string
	Class  string
}

// Client searches Alpaca assets and enriches with stock snapshots.
type Client struct {
	TradeBaseURL string
	DataBaseURL  string
	Key          string
	Secret       string
	HTTP         *http.Client
	TTL          time.Duration

	mu      sync.RWMutex
	assets  []Asset
	expires time.Time
}

// NewFromConfig builds a search client when Alpaca credentials exist.
// Returns nil when keys are missing.
func NewFromConfig(cfg *config.Config, httpClient *http.Client) *Client {
	if cfg == nil || strings.TrimSpace(cfg.AlpacaAPIKey) == "" || strings.TrimSpace(cfg.AlpacaAPISecret) == "" {
		return nil
	}
	trade := strings.TrimSpace(cfg.AlpacaBaseURL)
	if trade == "" {
		trade = "https://paper-api.alpaca.markets"
	}
	data := strings.TrimSpace(cfg.AlpacaDataBaseURL)
	if data == "" {
		data = "https://data.alpaca.markets"
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		TradeBaseURL: strings.TrimRight(trade, "/"),
		DataBaseURL:  strings.TrimRight(data, "/"),
		Key:          cfg.AlpacaAPIKey,
		Secret:       cfg.AlpacaAPISecret,
		HTTP:         httpClient,
		TTL:          defaultTTL,
	}
}

// Search returns up to maxResults matches for q, enriched with snapshots when possible.
func (c *Client) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Result{}, nil
	}
	if err := c.ensureAssets(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	matched := matchAssets(c.assets, q, maxResults)
	c.mu.RUnlock()

	out := make([]Result, 0, len(matched))
	for _, a := range matched {
		out = append(out, Result{
			Symbol:     a.Symbol,
			Name:       a.Name,
			AssetClass: a.Class,
		})
	}
	if len(out) == 0 {
		return out, nil
	}

	snaps, err := c.fetchSnapshots(ctx, symbolsOf(out))
	if err != nil {
		// Symbol/name still useful without quotes.
		return out, nil
	}
	for i := range out {
		if snap, ok := snaps[out[i].Symbol]; ok {
			applySnapshot(&out[i], snap)
		}
	}
	return out, nil
}

func (c *Client) ensureAssets(ctx context.Context) error {
	now := time.Now()
	c.mu.RLock()
	ok := c.assets != nil && now.Before(c.expires)
	c.mu.RUnlock()
	if ok {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.assets != nil && time.Now().Before(c.expires) {
		return nil
	}
	assets, err := c.fetchAssets(ctx)
	if err != nil {
		return err
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	c.assets = assets
	c.expires = time.Now().Add(ttl)
	return nil
}

type alpacaAsset struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Class  string `json:"class"`
	Status string `json:"status"`
}

func (c *Client) fetchAssets(ctx context.Context) ([]Asset, error) {
	raw, err := c.do(ctx, c.TradeBaseURL+assetsPath)
	if err != nil {
		return nil, err
	}
	var payload []alpacaAsset
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode assets: %w", err)
	}
	out := make([]Asset, 0, len(payload))
	for _, a := range payload {
		if a.Symbol == "" {
			continue
		}
		class := a.Class
		if class == "" {
			class = "us_equity"
		}
		out = append(out, Asset{Symbol: a.Symbol, Name: a.Name, Class: class})
	}
	return out, nil
}

type alpacaSnapshot struct {
	LatestTrade *struct {
		Price float64 `json:"p"`
	} `json:"latestTrade"`
	DailyBar *struct {
		Close float64 `json:"c"`
		Open  float64 `json:"o"`
	} `json:"dailyBar"`
	PrevDailyBar *struct {
		Close float64 `json:"c"`
	} `json:"prevDailyBar"`
}

func (c *Client) fetchSnapshots(ctx context.Context, symbols []string) (map[string]alpacaSnapshot, error) {
	if len(symbols) == 0 {
		return map[string]alpacaSnapshot{}, nil
	}
	u, err := url.Parse(c.DataBaseURL + snapshotsPath)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("symbols", strings.Join(symbols, ","))
	u.RawQuery = q.Encode()

	raw, err := c.do(ctx, u.String())
	if err != nil {
		return nil, err
	}
	var payload map[string]alpacaSnapshot
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode snapshots: %w", err)
	}
	return payload, nil
}

func (c *Client) do(ctx context.Context, fullURL string) ([]byte, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("APCA-API-KEY-ID", c.Key)
	req.Header.Set("APCA-API-SECRET-KEY", c.Secret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("alpaca GET %s returned %d: %s", fullURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func matchAssets(assets []Asset, q string, limit int) []Asset {
	needle := strings.ToUpper(strings.TrimSpace(q))
	var prefix, nameHits []Asset
	for _, a := range assets {
		sym := strings.ToUpper(a.Symbol)
		name := strings.ToUpper(a.Name)
		switch {
		case strings.HasPrefix(sym, needle):
			prefix = append(prefix, a)
		case strings.Contains(name, needle):
			nameHits = append(nameHits, a)
		}
	}
	sort.SliceStable(prefix, func(i, j int) bool {
		if len(prefix[i].Symbol) != len(prefix[j].Symbol) {
			return len(prefix[i].Symbol) < len(prefix[j].Symbol)
		}
		return prefix[i].Symbol < prefix[j].Symbol
	})
	out := append(prefix, nameHits...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func symbolsOf(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Symbol
	}
	return out
}

func applySnapshot(r *Result, snap alpacaSnapshot) {
	var price float64
	var ok bool
	if snap.LatestTrade != nil && snap.LatestTrade.Price > 0 {
		price = snap.LatestTrade.Price
		ok = true
	} else if snap.DailyBar != nil && snap.DailyBar.Close > 0 {
		price = snap.DailyBar.Close
		ok = true
	}
	if !ok {
		return
	}
	r.Price = &price
	if snap.PrevDailyBar == nil || snap.PrevDailyBar.Close <= 0 {
		return
	}
	prev := snap.PrevDailyBar.Close
	chg := price - prev
	pct := chg / prev * 100
	r.Change = &chg
	r.ChangePct = &pct
}
