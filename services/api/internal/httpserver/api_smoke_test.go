package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/auth"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type stubRunner struct {
	lastParams workflow.RunParams
	runID      uint
	err        error
}

func (s *stubRunner) RunWorkflow(_ context.Context, params workflow.RunParams) (uint, error) {
	s.lastParams = params
	if s.runID == 0 {
		s.runID = 42
	}
	if s.err != nil {
		return s.runID, s.err
	}
	return s.runID, nil
}

type fakeBroker struct {
	acct      broker.Account
	positions []broker.Position
	orders    []broker.Order
	acctErr   error
	posErr    error
	ordersErr error
}

func (f *fakeBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	if f.acctErr != nil {
		return broker.Account{}, f.acctErr
	}
	return f.acct, nil
}

func (f *fakeBroker) ListPositions(ctx context.Context) ([]broker.Position, error) {
	if f.posErr != nil {
		return nil, f.posErr
	}
	return f.positions, nil
}

func (f *fakeBroker) SubmitOrder(ctx context.Context, req broker.OrderRequest) (broker.Order, error) {
	return broker.Order{}, errors.New("not implemented")
}

func (f *fakeBroker) GetOrder(ctx context.Context, brokerOrderID string) (broker.Order, error) {
	return broker.Order{}, errors.New("not implemented")
}

func (f *fakeBroker) ListOrders(ctx context.Context, status string) ([]broker.Order, error) {
	if f.ordersErr != nil {
		return nil, f.ordersErr
	}
	return f.orders, nil
}

func defaultFakeBroker() *fakeBroker {
	return &fakeBroker{
		acct: broker.Account{Cash: 100000, Equity: 100000, PortfolioValue: 100000},
	}
}

func setupAPI(t *testing.T) (*gin.Engine, *gorm.DB, string, *stubRunner, *config.Config) {
	return setupAPIWithBroker(t, defaultFakeBroker(), httpserver.NoopSchedulerReloader{})
}

func setupAPIWithScheduler(t *testing.T, scheduler httpserver.SchedulerReloader) (*gin.Engine, *gorm.DB, string, *stubRunner, *config.Config) {
	return setupAPIWithBroker(t, defaultFakeBroker(), scheduler)
}

func setupAPIWithBroker(t *testing.T, br broker.Client, scheduler httpserver.SchedulerReloader) (*gin.Engine, *gorm.DB, string, *stubRunner, *config.Config) {
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
	runner := &stubRunner{runID: 99}
	strategiesSvc := &strategy.Service{DB: gormDB}

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:         gormDB,
		JWTSecret:  cfg.JWTSecret,
		Runner:     runner,
		Approvals:  approvalsSvc,
		Ledger:     ledgerSvc,
		Config:     cfg,
		Strategies: strategiesSvc,
		Scheduler:  scheduler,
		Broker:     br,
	})
	return router, gormDB, cfg.JWTSecret, runner, cfg
}

func bearerToken(t *testing.T, secret string, dbConn *gorm.DB) string {
	t.Helper()
	var user models.User
	if err := dbConn.Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("find admin: %v", err)
	}
	token, err := auth.IssueToken(secret, user.ID)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return token
}

func TestOverviewLiveNavMatchesWeights(t *testing.T) {
	fb := &fakeBroker{
		acct: broker.Account{Cash: 50000, Equity: 70000, PortfolioValue: 70000},
		positions: []broker.Position{{
			Symbol: "AAPL", Qty: 100, AvgCost: 150, MarketValue: 20000, CurrentPrice: 200, UnrealizedPL: 5000,
		}},
	}
	router, gormDB, secret, _, _ := setupAPIWithBroker(t, fb, httpserver.NoopSchedulerReloader{})
	token := bearerToken(t, secret, gormDB)

	if err := gormDB.Create(&models.NavSnapshot{
		TradeDate: "2026-07-25", Cash: 40000, Equity: 12000, Nav: 52000,
	}).Error; err != nil {
		t.Fatalf("create nav snapshot: %v", err)
	}
	run := models.WorkflowRun{TradeDate: "2026-07-25", Status: workflow.StatusExecuted}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}

	cash := resp["cash"].(float64)
	equity := resp["equity"].(float64)
	nav := resp["nav"].(float64)
	if cash != 50000 {
		t.Fatalf("cash: got %v want 50000 (broker, not ledger)", cash)
	}
	if equity != 20000 {
		t.Fatalf("equity: got %v want 20000", equity)
	}
	if nav != 70000 {
		t.Fatalf("nav: got %v want 70000", nav)
	}

	summary, ok := resp["positions_summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("positions_summary: got %T %v", resp["positions_summary"], resp["positions_summary"])
	}
	pos := summary[0].(map[string]any)
	wantWeight := 20000.0 / 70000.0
	if pos["weight"].(float64) != wantWeight {
		t.Fatalf("weight: got %v want %v", pos["weight"], wantWeight)
	}
	series, ok := resp["nav_series"].([]any)
	if !ok || len(series) != 1 {
		t.Fatalf("nav_series: got %v", resp["nav_series"])
	}
}

func TestPortfolioFromBrokerMergesStops(t *testing.T) {
	stop, take := 180.0, 220.0
	fb := &fakeBroker{
		acct: broker.Account{Cash: 50000, Equity: 70000, PortfolioValue: 70000},
		positions: []broker.Position{{
			Symbol: "AAPL", Qty: 100, AvgCost: 150, MarketValue: 20000, CurrentPrice: 200, UnrealizedPL: 5000,
		}},
	}
	router, gormDB, secret, _, _ := setupAPIWithBroker(t, fb, httpserver.NoopSchedulerReloader{})
	token := bearerToken(t, secret, gormDB)

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID, Symbol: "AAPL", Qty: 1, AvgCost: 1, StopLoss: &stop, TakeProfit: &take,
	}).Error; err != nil {
		t.Fatalf("create local position: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/portfolio", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp["cash"].(float64) != 50000 {
		t.Fatalf("cash: got %v want 50000", resp["cash"])
	}
	positions, ok := resp["positions"].([]any)
	if !ok || len(positions) != 1 {
		t.Fatalf("positions: got %v", resp["positions"])
	}
	p := positions[0].(map[string]any)
	if p["avg_cost"].(float64) != 150 || p["market_price"].(float64) != 200 {
		t.Fatalf("price fields: %+v", p)
	}
	if p["unrealized_pnl"].(float64) != 5000 {
		t.Fatalf("unrealized_pnl: got %v want 5000", p["unrealized_pnl"])
	}
	if p["stop_loss"].(float64) != 180 || p["take_profit"].(float64) != 220 {
		t.Fatalf("stops: %+v", p)
	}
	wantWeight := 20000.0 / 70000.0
	if p["weight"].(float64) != wantWeight {
		t.Fatalf("weight: got %v want %v", p["weight"], wantWeight)
	}
}

func TestOrdersFromBrokerMergesProposalID(t *testing.T) {
	fb := &fakeBroker{
		acct: broker.Account{Cash: 100000, Equity: 100000},
		orders: []broker.Order{{
			ID: "brk-1", ClientOrderID: "42", Symbol: "AAPL", Side: "buy",
			Qty: 10, FilledQty: 10, FilledAvgPrice: 191, Status: "filled",
		}},
	}
	router, gormDB, secret, _, _ := setupAPIWithBroker(t, fb, httpserver.NoopSchedulerReloader{})
	token := bearerToken(t, secret, gormDB)

	proposalID := uint(42)
	if err := gormDB.Create(&models.Order{
		AccountID: 1, Symbol: "AAPL", Side: "buy", Qty: 10,
		Status: "filled", BrokerOrderID: "brk-1", ClientOrderID: "42", ProposalID: &proposalID,
	}).Error; err != nil {
		t.Fatalf("create mirror: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	orders, ok := resp["orders"].([]any)
	if !ok || len(orders) != 1 {
		t.Fatalf("orders: got %v", resp["orders"])
	}
	o := orders[0].(map[string]any)
	if o["broker_order_id"] != "brk-1" || o["client_order_id"] != "42" {
		t.Fatalf("ids: %+v", o)
	}
	if o["symbol"] != "AAPL" || o["side"] != "buy" || o["status"] != "filled" {
		t.Fatalf("fields: %+v", o)
	}
	if o["qty"].(float64) != 10 || o["filled_qty"].(float64) != 10 || o["filled_avg_price"].(float64) != 191 {
		t.Fatalf("qty/fill: %+v", o)
	}
	if uint(o["proposal_id"].(float64)) != 42 {
		t.Fatalf("proposal_id: got %v want 42", o["proposal_id"])
	}
}

func TestBrokerNilReturns503(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPIWithBroker(t, nil, httpserver.NoopSchedulerReloader{})
	token := bearerToken(t, secret, gormDB)
	for _, path := range []string{"/api/v1/overview", "/api/v1/portfolio", "/api/v1/orders"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status got %d want 503 body=%s", path, w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s json: %v", path, err)
		}
		if resp["error"] != "alpaca not configured" {
			t.Fatalf("%s error: got %v", path, resp["error"])
		}
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	router, _, _, _, _ := setupAPI(t)
	paths := []string{
		"/api/v1/overview",
		"/api/v1/portfolio",
		"/api/v1/orders",
		"/api/v1/runs",
		"/api/v1/settings",
		"/api/v1/strategies",
		"/api/v1/approvals",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status got %d want 401", path, w.Code)
		}
	}
}

func TestOverviewPortfolioRunsSettingsSmoke(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	t.Run("overview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		for _, key := range []string{"cash", "equity", "nav", "pending_approvals_count", "positions_summary", "nav_series"} {
			if _, ok := resp[key]; !ok {
				t.Fatalf("missing key %q in %v", key, resp)
			}
		}
		if resp["cash"].(float64) != 100000 {
			t.Fatalf("cash: got %v want 100000", resp["cash"])
		}
	})

	t.Run("portfolio", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/portfolio", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if _, ok := resp["cash"]; !ok {
			t.Fatal("missing cash")
		}
		if _, ok := resp["positions"]; !ok {
			t.Fatal("missing positions")
		}
	})

	t.Run("orders", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if _, ok := resp["orders"]; !ok {
			t.Fatal("missing orders")
		}
	})

	t.Run("runs list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp []any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
	})

	t.Run("settings", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		wl, ok := resp["watchlist"].([]any)
		if !ok || len(wl) != 2 {
			t.Fatalf("watchlist: got %v", resp["watchlist"])
		}
		first, ok := wl[0].(map[string]any)
		if !ok {
			t.Fatalf("watchlist[0] want object, got %T %v", wl[0], wl[0])
		}
		if _, ok := first["symbol"].(string); !ok {
			t.Fatalf("watchlist[0].symbol: %v", first["symbol"])
		}
		if _, ok := first["can_hold"].(bool); !ok {
			t.Fatalf("watchlist[0].can_hold: %v", first["can_hold"])
		}
		if resp["market_data_provider"] != "free" {
			t.Fatalf("market_data_provider: got %v", resp["market_data_provider"])
		}
		if _, ok := resp["risk_rules"].(map[string]any); !ok {
			t.Fatalf("risk_rules: got %T %v", resp["risk_rules"], resp["risk_rules"])
		}
	})
}

func TestRunsDetailAndTriggerAndCancel(t *testing.T) {
	router, gormDB, secret, runner, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	st := models.Strategy{Name: "Test Strategy", IsActive: true, ExecutionMode: "require_approval"}
	if err := gormDB.Create(&st).Error; err != nil {
		t.Fatalf("create strategy: %v", err)
	}
	run := models.WorkflowRun{
		TradeDate:  "2026-07-25",
		Status:     workflow.StatusAwaitingApproval,
		StrategyID: &st.ID,
		Trigger:    workflow.TriggerManual,
	}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := gormDB.Create(&models.WorkflowStepResult{
		RunID: run.ID, Step: workflow.StepAnalyst, Status: workflow.StepStatusOK,
		PayloadJSON: `{"result":{"items":[]},"trace":{"agent":"analyst","rounds":[],"stop_reason":"final"}}`,
	}).Error; err != nil {
		t.Fatalf("create step: %v", err)
	}
	proposal := models.TradeProposal{
		RunID: run.ID, Symbol: "AAPL", Side: "buy", Qty: 10,
		Status: workflow.ProposalAwaitingApproval, EstimatedNotional: 1900,
	}
	if err := gormDB.Create(&proposal).Error; err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	t.Run("run detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/runs/%d", run.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		for _, key := range []string{"id", "trade_date", "status", "strategy_id", "strategy_name", "trigger", "steps", "proposals", "orders"} {
			if _, ok := resp[key]; !ok {
				t.Fatalf("missing key %q", key)
			}
		}
		if resp["strategy_name"] != "Test Strategy" {
			t.Fatalf("strategy_name: got %v want Test Strategy", resp["strategy_name"])
		}
		if resp["trigger"] != workflow.TriggerManual {
			t.Fatalf("trigger: got %v want %s", resp["trigger"], workflow.TriggerManual)
		}
		steps, ok := resp["steps"].([]any)
		if !ok || len(steps) == 0 {
			t.Fatalf("steps: got %T %v", resp["steps"], resp["steps"])
		}
		step0, ok := steps[0].(map[string]any)
		if !ok {
			t.Fatalf("step[0]: got %T", steps[0])
		}
		if _, ok := step0["payload_json"]; !ok {
			t.Fatalf("missing payload_json on step: %v", step0)
		}
		ca, _ := resp["created_at"].(string)
		if ca == "" {
			t.Fatal("created_at empty")
		}
		if _, err := time.Parse(time.RFC3339, ca); err != nil {
			if _, err2 := time.Parse(time.RFC3339Nano, ca); err2 != nil {
				t.Fatalf("created_at not RFC3339: %q err=%v", ca, err)
			}
		}
	})

	t.Run("runs list enriched", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
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
		if len(resp) == 0 {
			t.Fatal("expected at least one run")
		}
		for _, key := range []string{"strategy_id", "strategy_name", "trigger"} {
			if _, ok := resp[0][key]; !ok {
				t.Fatalf("missing key %q", key)
			}
		}
		if resp[0]["strategy_name"] != "Test Strategy" {
			t.Fatalf("strategy_name: got %v want Test Strategy", resp[0]["strategy_name"])
		}
		var listCA string
		for _, item := range resp {
			if uint(item["id"].(float64)) == run.ID {
				listCA, _ = item["created_at"].(string)
				break
			}
		}
		if listCA == "" {
			t.Fatal("created_at empty in list")
		}
		if _, err := time.Parse(time.RFC3339, listCA); err != nil {
			if _, err2 := time.Parse(time.RFC3339Nano, listCA); err2 != nil {
				t.Fatalf("created_at not RFC3339 in list: %q err=%v", listCA, err)
			}
		}
	})

	t.Run("post trigger", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-24"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/trigger", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if uint(resp["run_id"].(float64)) != 99 {
			t.Fatalf("run_id: got %v want 99", resp["run_id"])
		}
		if runner.lastParams.TradeDate != "2026-07-24" {
			t.Fatalf("trade_date: got %q", runner.lastParams.TradeDate)
		}
		if runner.lastParams.Trigger != workflow.TriggerManual {
			t.Fatalf("trigger: got %q want %s", runner.lastParams.Trigger, workflow.TriggerManual)
		}
		if runner.lastParams.ExecutionMode != "" {
			t.Fatalf("execution_mode: got %q want empty (runner resolves from DB)", runner.lastParams.ExecutionMode)
		}
		var active models.Strategy
		if err := gormDB.Where("is_active = ?", true).First(&active).Error; err != nil {
			t.Fatalf("active strategy: %v", err)
		}
		if runner.lastParams.StrategyID == nil || *runner.lastParams.StrategyID != active.ID {
			t.Fatalf("strategy_id: got %v want %d", runner.lastParams.StrategyID, active.ID)
		}
	})

	t.Run("post trigger failure includes run_id", func(t *testing.T) {
		runner.err = errors.New("mid-fill boom")
		runner.runID = 77
		t.Cleanup(func() {
			runner.err = nil
			runner.runID = 99
		})
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-26"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/trigger", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status: got %d want 500 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if resp["error"] == nil || resp["error"] == "" {
			t.Fatalf("expected error field: %v", resp)
		}
		if uint(resp["run_id"].(float64)) != 77 {
			t.Fatalf("run_id: got %v want 77", resp["run_id"])
		}
	})

	t.Run("cancel", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/runs/%d/cancel", run.ID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if resp["ok"] != true {
			t.Fatalf("ok: got %v", resp["ok"])
		}
	})
}

func TestApprovalsListAndDecide(t *testing.T) {
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	run := models.WorkflowRun{TradeDate: "2026-07-25", Status: workflow.StatusAwaitingApproval}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := gormDB.Create(&models.WorkflowStepResult{
		RunID: run.ID, Step: workflow.StepAnalyst, Status: workflow.StepStatusOK,
		PayloadJSON: `{"result":{"items":[]},"trace":{"agent":"analyst","rounds":[],"stop_reason":"final"}}`,
	}).Error; err != nil {
		t.Fatalf("create step: %v", err)
	}
	proposal := models.TradeProposal{
		RunID: run.ID, Symbol: "AAPL", Side: "buy", Qty: 10,
		Status: workflow.ProposalAwaitingApproval, EstimatedNotional: 1900,
	}
	if err := gormDB.Create(&proposal).Error; err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	approval := models.Approval{
		ProposalID: proposal.ID, Status: workflow.ApprovalPending,
		BreachReasonsJSON: `["max_order_notional"]`,
	}
	if err := gormDB.Create(&approval).Error; err != nil {
		t.Fatalf("create approval: %v", err)
	}

	t.Run("list pending", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/approvals?status=pending", nil)
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
			t.Fatalf("len: got %d want 1", len(resp))
		}
		item := resp[0]
		if item["symbol"] != "AAPL" || item["side"] != "buy" {
			t.Fatalf("item: %+v", item)
		}
		if _, ok := item["breach_reasons"]; !ok {
			t.Fatal("missing breach_reasons")
		}
	})

	t.Run("decide reject", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"decision": "rejected", "note": "nope"})
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/approvals/%d/decide", approval.ID), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if resp["ok"] != true {
			t.Fatalf("ok: got %v", resp["ok"])
		}
	})
}

func TestInternalTriggerRunRequiresToken(t *testing.T) {
	router, _, _, runner, cfg := setupAPI(t)

	t.Run("unauthorized without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/runs/trigger", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d want 401", w.Code)
		}
	})

	t.Run("success with internal token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-23"})
		req := httptest.NewRequest(http.MethodPost, "/internal/runs/trigger", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.InternalRunToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d want 200 body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if uint(resp["run_id"].(float64)) != 99 {
			t.Fatalf("run_id: got %v", resp["run_id"])
		}
		if runner.lastParams.TradeDate != "2026-07-23" {
			t.Fatalf("trade_date: got %q", runner.lastParams.TradeDate)
		}
		if runner.lastParams.Trigger != workflow.TriggerManual {
			t.Fatalf("trigger: got %q want %s", runner.lastParams.Trigger, workflow.TriggerManual)
		}
	})
}
