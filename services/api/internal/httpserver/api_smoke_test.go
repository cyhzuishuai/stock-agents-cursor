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

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/auth"
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
	lastTradeDate string
	runID         uint
	err           error
}

func (s *stubRunner) RunEOD(_ context.Context, params workflow.RunParams) (uint, error) {
	s.lastTradeDate = params.TradeDate
	if s.runID == 0 {
		s.runID = 42
	}
	if s.err != nil {
		return s.runID, s.err
	}
	return s.runID, nil
}

func setupAPI(t *testing.T) (*gin.Engine, *gorm.DB, string, *stubRunner, *config.Config) {
	return setupAPIWithScheduler(t, httpserver.NoopSchedulerReloader{})
}

func setupAPIWithScheduler(t *testing.T, scheduler httpserver.SchedulerReloader) (*gin.Engine, *gorm.DB, string, *stubRunner, *config.Config) {
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
	router, gormDB, secret, _, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	account.Cash = 50000
	if err := gormDB.Save(&account).Error; err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID, Symbol: "AAPL", Qty: 100, AvgCost: 150,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}
	if err := gormDB.Create(&models.NavSnapshot{
		TradeDate: "2026-07-25", Cash: 40000, Equity: 12000, Nav: 52000,
	}).Error; err != nil {
		t.Fatalf("create nav snapshot: %v", err)
	}
	run := models.WorkflowRun{TradeDate: "2026-07-25", Status: workflow.StatusExecuted}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := gormDB.Create(&models.WorkflowStepResult{
		RunID: run.ID, Step: workflow.StepData, Status: workflow.StepStatusOK,
		PayloadJSON: `{"bars":[{"symbol":"AAPL","close":200}]}`,
	}).Error; err != nil {
		t.Fatalf("create step: %v", err)
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
		t.Fatalf("cash: got %v want 50000 (live, not snapshot)", cash)
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
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	router, _, _, _, _ := setupAPI(t)
	paths := []string{
		"/api/v1/overview",
		"/api/v1/portfolio",
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
		if resp["market_data_provider"] != "free" {
			t.Fatalf("market_data_provider: got %v", resp["market_data_provider"])
		}
		if _, ok := resp["risk_rules"].(map[string]any); !ok {
			t.Fatalf("risk_rules: got %T %v", resp["risk_rules"], resp["risk_rules"])
		}
	})
}

func TestRunsDetailAndEODAndCancel(t *testing.T) {
	router, gormDB, secret, runner, _ := setupAPI(t)
	token := bearerToken(t, secret, gormDB)

	run := models.WorkflowRun{TradeDate: "2026-07-25", Status: workflow.StatusAwaitingApproval}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := gormDB.Create(&models.WorkflowStepResult{
		RunID: run.ID, Step: workflow.StepData, Status: workflow.StepStatusOK,
		PayloadJSON: `{"bars":[{"symbol":"AAPL","close":190}]}`,
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
		for _, key := range []string{"id", "trade_date", "status", "steps", "proposals", "orders"} {
			if _, ok := resp[key]; !ok {
				t.Fatalf("missing key %q", key)
			}
		}
	})

	t.Run("post eod", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-24"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/eod", bytes.NewReader(body))
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
		if runner.lastTradeDate != "2026-07-24" {
			t.Fatalf("trade_date: got %q", runner.lastTradeDate)
		}
	})

	t.Run("post eod failure includes run_id", func(t *testing.T) {
		runner.err = errors.New("mid-fill boom")
		runner.runID = 77
		t.Cleanup(func() {
			runner.err = nil
			runner.runID = 99
		})
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-26"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/eod", bytes.NewReader(body))
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
		RunID: run.ID, Step: workflow.StepData, Status: workflow.StepStatusOK,
		PayloadJSON: `{"bars":[{"symbol":"AAPL","close":190}]}`,
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

func TestInternalEODRequiresToken(t *testing.T) {
	router, _, _, runner, cfg := setupAPI(t)

	t.Run("unauthorized without token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/eod/run", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d want 401", w.Code)
		}
	})

	t.Run("success with internal token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"trade_date": "2026-07-23"})
		req := httptest.NewRequest(http.MethodPost, "/internal/eod/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", cfg.InternalEODToken)
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
		if runner.lastTradeDate != "2026-07-23" {
			t.Fatalf("trade_date: got %q", runner.lastTradeDate)
		}
	})
}
