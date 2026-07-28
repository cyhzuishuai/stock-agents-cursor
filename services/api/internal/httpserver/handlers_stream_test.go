package httpserver_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/stream"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestStreamEndpointsRequireAuth(t *testing.T) {
	router, _, _, _, _ := setupAPI(t)

	for _, path := range []string{"/api/v1/stream/market", "/api/v1/stream/account"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", path, w.Code)
		}
	}
}

func TestStreamEndpointsDisabledReturn503(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	for _, path := range []string{"/api/v1/stream/market", "/api/v1/stream/account"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", path, w.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v body=%q", path, err, w.Body.String())
		}
		if body["error"] != "streaming disabled" {
			t.Fatalf("%s: error = %q", path, body["error"])
		}
	}
}

func TestStreamMarketFansHubEvents(t *testing.T) {
	hub := stream.NewHub(true, "key", "secret")
	if err := hub.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	router, gormDB, secret := setupStreamRouter(t, hub)
	token := bearerToken(t, secret, gormDB)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream/market", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	go publishUntilDone(ctx, hub, `{"symbol":"AAPL","p":1.23}`)

	gotEvent, gotData := readSSEUntil(t, resp.Body, 2*time.Second, func(event, data string) bool {
		return event == "quote" && data == `{"symbol":"AAPL","p":1.23}`
	})
	cancel()
	if gotEvent != "quote" || gotData != `{"symbol":"AAPL","p":1.23}` {
		t.Fatalf("got event=%q data=%q", gotEvent, gotData)
	}
}

func TestStreamAccountFansHubEvents(t *testing.T) {
	hub := stream.NewHub(true, "key", "secret")
	router, gormDB, secret := setupStreamRouter(t, hub)
	token := bearerToken(t, secret, gormDB)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream/account", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	go publishAccountUntilDone(ctx, hub, `{"event":"trade_update"}`)

	gotEvent, gotData := readSSEUntil(t, resp.Body, 2*time.Second, func(event, data string) bool {
		return event == "account" && data == `{"event":"trade_update"}`
	})
	cancel()
	if gotEvent != "account" || gotData != `{"event":"trade_update"}` {
		t.Fatalf("got event=%q data=%q", gotEvent, gotData)
	}
}

func TestStreamHeartbeatComment(t *testing.T) {
	prev := httpserver.SetSSEHeartbeatIntervalForTest(20 * time.Millisecond)
	defer httpserver.SetSSEHeartbeatIntervalForTest(prev)

	hub := stream.NewHub(true, "key", "secret")
	router, gormDB, secret := setupStreamRouter(t, hub)
	token := bearerToken(t, secret, gormDB)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/stream/market", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}

	deadline := time.Now().Add(2 * time.Second)
	reader := bufio.NewReader(resp.Body)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		if strings.HasPrefix(strings.TrimRight(line, "\r\n"), ": heartbeat") {
			return
		}
	}
	t.Fatal("timed out waiting for heartbeat comment")
}

func setupStreamRouter(t *testing.T, hub *stream.Hub) (*gin.Engine, *gorm.DB, string) {
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
		InternalRunToken:        "internal-secret",
		JWTSecret:               "test-jwt-secret",
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	ledgerSvc := &ledger.Service{DB: gormDB}
	approvalsSvc := &approvals.Service{DB: gormDB, Ledger: ledgerSvc}
	strategiesSvc := &strategy.Service{DB: gormDB}
	runner := &stubRunner{runID: 99}

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:         gormDB,
		JWTSecret:  cfg.JWTSecret,
		Runner:     runner,
		Approvals:  approvalsSvc,
		Ledger:     ledgerSvc,
		Config:     cfg,
		Strategies: strategiesSvc,
		Scheduler:  httpserver.NoopSchedulerReloader{},
		Broker:     defaultFakeBroker(),
		Stream:     hub,
	})
	return router, gormDB, cfg.JWTSecret
}

func publishUntilDone(ctx context.Context, hub *stream.Hub, payload string) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Unique symbols bypass the hub's ≥1s per-symbol throttle while
			// the SSE handler is still subscribing.
			hub.PublishQuote(fmt.Sprintf("S%d", i), []byte(payload))
			i++
		}
	}
}

func publishAccountUntilDone(ctx context.Context, hub *stream.Hub, payload string) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hub.PublishAccount([]byte(payload))
		}
	}
}

func readSSEUntil(t *testing.T, r io.Reader, timeout time.Duration, match func(event, data string) bool) (event, data string) {
	t.Helper()
	type result struct {
		event string
		data  string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(r)
		var curEvent string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				ch <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "event:"):
				curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				curData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if match(curEvent, curData) {
					ch <- result{event: curEvent, data: curData}
					return
				}
			}
		}
	}()

	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read SSE: %v", got.err)
		}
		return got.event, got.data
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for matching SSE event")
		return "", ""
	}
}
