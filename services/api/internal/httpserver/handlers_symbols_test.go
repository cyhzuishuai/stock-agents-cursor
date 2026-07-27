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
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type mockYahooTransport struct {
	body       string
	statusCode int
	err        error
	lastURL    string
}

func (m *mockYahooTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.lastURL = req.URL.String()
	if m.err != nil {
		return nil, m.err
	}
	status := m.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     make(http.Header),
	}, nil
}

func TestSymbolSearch(t *testing.T) {
	t.Run("empty q returns empty array", func(t *testing.T) {
		mock := &mockYahooTransport{}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		if mock.lastURL != "" {
			t.Fatalf("expected no upstream call for empty q, got %q", mock.lastURL)
		}
		var resp []map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if len(resp) != 0 {
			t.Fatalf("expected [], got %v", resp)
		}
	})

	t.Run("returns mapped equity quote", func(t *testing.T) {
		mock := &mockYahooTransport{
			body: `{"quotes":[{"symbol":"AAPL","shortname":"Apple Inc.","quoteType":"EQUITY","exchange":"NMS"}]}`,
		}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(mock.lastURL, "q=aap") {
			t.Fatalf("upstream URL: got %q", mock.lastURL)
		}
		var resp []map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if len(resp) != 1 {
			t.Fatalf("len: got %d want 1", len(resp))
		}
		if resp[0]["symbol"] != "AAPL" || resp[0]["name"] != "Apple Inc." {
			t.Fatalf("result: got %+v", resp[0])
		}
	})

	t.Run("prefers EQUITY over other quote types", func(t *testing.T) {
		mock := &mockYahooTransport{
			body: `{"quotes":[
				{"symbol":"AAP","shortname":"Advance Auto Parts","quoteType":"EQUITY"},
				{"symbol":"AAPL.NEWS","shortname":"Apple News","quoteType":"NEWS"}
			]}`,
		}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock)
		token := bearerToken(t, secret, gormDB)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=aap", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
		}
		var resp []map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if len(resp) != 1 || resp[0]["symbol"] != "AAP" {
			t.Fatalf("expected only EQUITY quote, got %v", resp)
		}
	})

	t.Run("upstream failure returns 502", func(t *testing.T) {
		mock := &mockYahooTransport{err: io.ErrUnexpectedEOF}
		router, gormDB, secret := setupSymbolSearchRouter(t, mock)
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

func setupSymbolSearchRouter(t *testing.T, mock *mockYahooTransport) (*gin.Engine, *gorm.DB, string) {
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
		MarketDataProvider:      "free",
		InternalEODToken:        "internal-secret",
		JWTSecret:               "test-jwt-secret",
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	ledgerSvc := &ledger.Service{DB: gormDB}
	approvalsSvc := &approvals.Service{DB: gormDB, Ledger: ledgerSvc}
	strategiesSvc := &strategy.Service{DB: gormDB}

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:         gormDB,
		JWTSecret:  cfg.JWTSecret,
		Runner:     &stubRunner{runID: 99},
		Approvals:  approvalsSvc,
		Ledger:     ledgerSvc,
		Config:     cfg,
		Strategies: strategiesSvc,
		Scheduler:  httpserver.NoopSchedulerReloader{},
		HTTPClient: &http.Client{Transport: mock},
	})
	return router, gormDB, cfg.JWTSecret
}
