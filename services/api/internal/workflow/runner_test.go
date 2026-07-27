package workflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const tradeDate = "2026-07-22"

func TestRunEODHappyPathAutoExecBuy(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})

	runID, err := env.runner.RunEOD(context.Background(), tradeDate)
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}
	if runID == 0 {
		t.Fatal("expected non-zero runID")
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want %s", run.Status, workflow.StatusExecuted)
	}

	var steps []models.WorkflowStepResult
	if err := env.db.Where("run_id = ?", runID).Order("id").Find(&steps).Error; err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 5 {
		t.Fatalf("steps count: got %d want 5", len(steps))
	}
	wantSteps := []string{
		workflow.StepData, workflow.StepResearch, workflow.StepDecision,
		workflow.StepPortfolio, workflow.StepRisk,
	}
	for i, s := range steps {
		if s.Step != wantSteps[i] || s.Status != workflow.StepStatusOK {
			t.Fatalf("step[%d]: got %s/%s want %s/ok", i, s.Step, s.Status, wantSteps[i])
		}
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: got %+v", proposals)
	}

	var orders []models.Order
	if err := env.db.Find(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: got %d want 1", len(orders))
	}
	if orders[0].FillPrice != 191 || orders[0].Qty != 10 || orders[0].Side != "buy" {
		t.Fatalf("order: %+v", orders[0])
	}

	var account models.Account
	if err := env.db.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000-1910 {
		t.Fatalf("cash: got %v want %v", account.Cash, 100000-1910)
	}

	var nav models.NavSnapshot
	if err := env.db.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Nav != 100000 {
		t.Fatalf("nav: got %v want 100000", nav.Nav)
	}

	var approvals int64
	if err := env.db.Model(&models.Approval{}).Count(&approvals).Error; err != nil {
		t.Fatalf("approvals count: %v", err)
	}
	if approvals != 0 {
		t.Fatalf("approvals: got %d want 0", approvals)
	}
}

func TestRunEODAgentFailureMidChainNoFills(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  "", // failure
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[],"warnings":[]}`,
		failAt:    workflow.StepDecision,
	})

	runID, err := env.runner.RunEOD(context.Background(), tradeDate)
	if err == nil {
		t.Fatal("expected error from failed agent")
	}
	if runID == 0 {
		t.Fatal("expected runID even on failure")
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed", run.Status)
	}
	if run.ErrorMsg == "" {
		t.Fatal("expected error_msg")
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}

	var proposals int64
	if err := env.db.Model(&models.TradeProposal{}).Count(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if proposals != 0 {
		t.Fatalf("proposals: got %d want 0", proposals)
	}

	var account models.Account
	if err := env.db.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash mutated: got %v", account.Cash)
	}
}

func TestRunEODBreachCreatesPendingApprovalNoFill(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["large"],"scores":{},"suggested_action":"review"}],"warnings":[]}`,
	})

	runID, err := env.runner.RunEOD(context.Background(), tradeDate)
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusAwaitingApproval {
		t.Fatalf("run status: got %q want awaiting_approval", run.Status)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalAwaitingApproval {
		t.Fatalf("proposal: %+v", proposals)
	}

	var approvals []models.Approval
	if err := env.db.Find(&approvals).Error; err != nil {
		t.Fatalf("approvals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Status != workflow.ApprovalPending {
		t.Fatalf("approval: %+v", approvals)
	}
	var reasons []string
	if err := json.Unmarshal([]byte(approvals[0].BreachReasonsJSON), &reasons); err != nil {
		t.Fatalf("breach reasons json: %v", err)
	}
	if len(reasons) == 0 {
		t.Fatal("expected breach reasons")
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}

	var account models.Account
	if err := env.db.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash should be unchanged: got %v", account.Cash)
	}

	var nav models.NavSnapshot
	if err := env.db.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Cash != 100000 || nav.Equity != 0 || nav.Nav != 100000 {
		t.Fatalf("nav: %+v", nav)
	}
}

type stubResponses struct {
	data, research, decision, portfolio, risk string
	failAt                                    string
}

type runnerEnv struct {
	db     *gorm.DB
	runner *workflow.Runner
}

func setupRunnerEnv(t *testing.T, stubs stubResponses) *runnerEnv {
	t.Helper()

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
		Watchlist:               []string{"AAPL"},
		RiskMaxOrderNotional:    10000,
		RiskMaxSingleNameWeight: 0.20,
		RiskMinCashRatio:        0.10,
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	servers := startAgentStubs(t, stubs)

	runner := &workflow.Runner{
		DB: gormDB,
		Agents: &agentsclient.Client{
			DataURL:      servers.data.URL,
			ResearchURL:  servers.research.URL,
			DecisionURL:  servers.decision.URL,
			PortfolioURL: servers.portfolio.URL,
			RiskURL:      servers.risk.URL,
			MaxRetries:   0,
		},
		Ledger: &ledger.Service{DB: gormDB},
		Risk: risk.LoadEngineFromMap(map[string]float64{
			"max_order_notional":     10000,
			"max_single_name_weight": 0.20,
			"min_cash_ratio":         0.10,
		}),
		Redis: rdb,
	}
	return &runnerEnv{db: gormDB, runner: runner}
}

type agentServers struct {
	data, research, decision, portfolio, risk *httptest.Server
}

func startAgentStubs(t *testing.T, stubs stubResponses) agentServers {
	t.Helper()
	makeServer := func(step, body string) *httptest.Server {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.URL.Path != "/v1/run" {
				http.NotFound(w, r)
				return
			}
			if stubs.failAt == step {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	return agentServers{
		data:      makeServer(workflow.StepData, stubs.data),
		research:  makeServer(workflow.StepResearch, stubs.research),
		decision:  makeServer(workflow.StepDecision, stubs.decision),
		portfolio: makeServer(workflow.StepPortfolio, stubs.portfolio),
		risk:      makeServer(workflow.StepRisk, stubs.risk),
	}
}

func dataBarsJSON(close float64) string {
	return fmt.Sprintf(`{"bars":[{"symbol":"AAPL","trade_date":"%s","open":190,"high":192,"low":188,"close":%v,"volume":1000000}],"warnings":[]}`, tradeDate, close)
}

func portfolioBuyJSON(qty, notional, cashImpact float64) string {
	return fmt.Sprintf(
		`{"proposals":[{"symbol":"AAPL","side":"buy","qty":%v,"estimated_notional":%v,"estimated_cash_impact":%v}],"warnings":[]}`,
		qty, notional, cashImpact,
	)
}
