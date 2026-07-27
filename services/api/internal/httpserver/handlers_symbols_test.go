package httpserver_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/symbolsearch"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type mockAlpacaTransport struct {
	assetsBody     string
	snapshotsBody  string
	assetsStatus   int
	snapshotsStatus int
	err            error
	paths          []string
}

func (m *mockAlpacaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.paths = append(m.paths, req.URL.Path)
	if m.err != nil {
		return nil, m.err
	}
	status := http.StatusOK
	body := "{}"
	switch {
	case strings.Contains(req.URL.Path, "/v2/assets"):
		if m.assetsStatus != 0 {
			status = m.assetsStatus
		}
		if m.assetsBody != "" {
			body = m.assetsBody
		}
	case strings.Contains(req.URL.Path, "/v2/stocks/snapshots"):
		if m.snapshotsStatus != 0 {
			status = m.snapshotsStatus
		}
		if m.snapshotsBody != "" {
			body = m.snapshotsBody
		}
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestSymbolSearch(t *testing.T) {
	t.Run("empty q returns empty array", func(t *testing.T) {
		mock := &mockAlpacaTransport{}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock, true)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		if len(mock.paths) != 0 {
			t.Fatalf("expected no upstream call for empty q, got %v", mock.paths)
		}
		var resp []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if len(resp) != 0 {
			t.Fatalf("expected [], got %v", resp)
		}
	})

	t.Run("missing alpaca returns 503", func(t *testing.T) {
		router, gormDB, secret := setupSymbolSearchRouter(t, nil, false)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status: got %d want 503 body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("returns mapped asset with quote", func(t *testing.T) {
		mock := &mockAlpacaTransport{
			assetsBody: `[{"symbol":"AAPL","name":"Apple Inc.","class":"us_equity","status":"active"},
				{"symbol":"AAAA","name":"Amplius ETF","class":"us_equity","status":"active"}]`,
			snapshotsBody: `{"AAPL":{"latestTrade":{"p":190.5},"prevDailyBar":{"c":189.0}}}`,
		}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock, true)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if len(resp) != 1 {
			t.Fatalf("len: got %d want 1 (%v)", len(resp), resp)
		}
		if resp[0]["symbol"] != "AAPL" || resp[0]["name"] != "Apple Inc." {
			t.Fatalf("result: got %+v", resp[0])
		}
		if resp[0]["price"].(float64) != 190.5 {
			t.Fatalf("price: got %v", resp[0]["price"])
		}
	})

	t.Run("prefix ranking prefers shorter symbol", func(t *testing.T) {
		mock := &mockAlpacaTransport{
			assetsBody: `[{"symbol":"AAPL","name":"Apple Inc.","class":"us_equity"},
				{"symbol":"AAP","name":"Advance Auto Parts","class":"us_equity"}]`,
			snapshotsBody: `{}`,
		}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock, true)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
		}
		var resp []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if len(resp) != 2 || resp[0]["symbol"] != "AAP" {
			t.Fatalf("expected AAP first, got %v", resp)
		}
	})

	t.Run("snapshot failure still returns symbol name", func(t *testing.T) {
		mock := &mockAlpacaTransport{
			assetsBody:      `[{"symbol":"MSFT","name":"Microsoft Corporation","class":"us_equity"}]`,
			snapshotsStatus: http.StatusInternalServerError,
			snapshotsBody:   "err",
		}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock, true)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=msft", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
		}
		var resp []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if len(resp) != 1 || resp[0]["symbol"] != "MSFT" {
			t.Fatalf("got %v", resp)
		}
		if resp[0]["price"] != nil {
			t.Fatalf("expected null price, got %v", resp[0]["price"])
		}
	})

	t.Run("upstream asset failure returns 502", func(t *testing.T) {
		mock := &mockAlpacaTransport{err: io.ErrUnexpectedEOF}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock, true)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadGateway {
			t.Fatalf("status: got %d want 502 body=%s", w.Code, w.Body.String())
		}
	})
}

func setupSymbolSearchRouter(t *testing.T, mock *mockAlpacaTransport, withSearch bool) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	cfg := &config.Config{
		AdminUsername:           "admin",
		AdminPassword:           "admin123",
		InitialCash:             100000,
		Watchlist:               []string{"AAPL", "MSFT"},
		RiskMaxOrderNotional:    10000,
		RiskMaxSingleNameWeight: 0.20,
		RiskMinCashRatio:        0.10,
		MarketDataProvider:      "alpaca",
		InternalEODToken:        "internal-secret",
		JWTSecret:               "test-jwt-secret",
		AlpacaAPIKey:            "test-key",
		AlpacaAPISecret:         "test-secret",
		AlpacaBaseURL:           "https://paper.test",
		AlpacaDataBaseURL:       "https://data.test",
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	ledgerSvc := &ledger.Service{DB: gormDB}
	approvalsSvc := &approvals.Service{DB: gormDB, Ledger: ledgerSvc}
	strategiesSvc := &strategy.Service{DB: gormDB}

	var searcher *symbolsearch.Client
	if withSearch {
		httpClient := &http.Client{Transport: mock}
		searcher = symbolsearch.NewFromConfig(cfg, httpClient)
	}

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:           gormDB,
		JWTSecret:    cfg.JWTSecret,
		Runner:       &stubRunner{runID: 99},
		Approvals:    approvalsSvc,
		Ledger:       ledgerSvc,
		Config:       cfg,
		Strategies:   strategiesSvc,
		Scheduler:    httpserver.NoopSchedulerReloader{},
		SymbolSearch: searcher,
	})
	return router, gormDB, cfg.JWTSecret
}
