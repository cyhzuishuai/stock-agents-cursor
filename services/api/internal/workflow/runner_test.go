package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func eodParams(td string) workflow.RunParams {
	return workflow.RunParams{TradeDate: td}
}

func TestRunEODHappyPathAutoExecBuy(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
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

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
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

	runID, err := env.runner.RunEOD(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeRequireApproval,
	})
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

func TestRunEODUnderstatedNotionalStillBreachesMaxOrder(t *testing.T) {
	// Agent understates notional (100) but qty*close = 100*191 = 19100 > max 10000.
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(100, 100, -100),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["large"],"scores":{},"suggested_action":"auto"}],"warnings":[]}`,
	})

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
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
	if len(approvals) != 1 {
		t.Fatalf("approvals: got %d want 1", len(approvals))
	}
	var reasons []string
	if err := json.Unmarshal([]byte(approvals[0].BreachReasonsJSON), &reasons); err != nil {
		t.Fatalf("breach reasons json: %v", err)
	}
	found := false
	for _, r := range reasons {
		if r == "max_order_notional" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected max_order_notional breach, got %v", reasons)
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}
}

func TestRunEODResolvesExecutionModeFromStrategyDB(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["large"],"scores":{},"suggested_action":"review"}],"warnings":[]}`,
	})

	st := models.Strategy{
		Name:          "auto-reject-test",
		IsActive:      true,
		ExecutionMode: workflow.ExecutionModeAutoReject,
	}
	if err := env.db.Create(&st).Error; err != nil {
		t.Fatalf("create strategy: %v", err)
	}

	runID, err := env.runner.RunEOD(context.Background(), workflow.RunParams{
		TradeDate:  tradeDate,
		StrategyID: &st.ID,
		Trigger:    workflow.TriggerManual,
		// ExecutionMode intentionally empty — runner must load from DB.
	})
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalRejected {
		t.Fatalf("proposal: %+v", proposals)
	}

	var approvals int64
	if err := env.db.Model(&models.Approval{}).Count(&approvals).Error; err != nil {
		t.Fatalf("approvals count: %v", err)
	}
	if approvals != 0 {
		t.Fatalf("approvals: got %d want 0", approvals)
	}
}

func TestRunEODAutoRejectBreachesRejectsProposalNoApproval(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["large"],"scores":{},"suggested_action":"review"}],"warnings":[]}`,
	})

	sid := uint(1)
	runID, err := env.runner.RunEOD(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		StrategyID:    &sid,
		Trigger:       workflow.TriggerManual,
		ExecutionMode: workflow.ExecutionModeAutoReject,
	})
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}
	if run.StrategyID == nil || *run.StrategyID != sid {
		t.Fatalf("strategy_id: got %v want %d", run.StrategyID, sid)
	}
	if run.Trigger != workflow.TriggerManual {
		t.Fatalf("trigger: got %q want %s", run.Trigger, workflow.TriggerManual)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalRejected {
		t.Fatalf("proposal: %+v", proposals)
	}
	var reasons []string
	if err := json.Unmarshal([]byte(proposals[0].BreachReasonsJSON), &reasons); err != nil {
		t.Fatalf("breach reasons json: %v", err)
	}
	if len(reasons) == 0 {
		t.Fatal("expected breach reasons on proposal")
	}

	var approvals int64
	if err := env.db.Model(&models.Approval{}).Count(&approvals).Error; err != nil {
		t.Fatalf("approvals count: %v", err)
	}
	if approvals != 0 {
		t.Fatalf("approvals: got %d want 0", approvals)
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}
}

func TestRunEODMidFillFailureSetsTerminalFailed(t *testing.T) {
	// First symbol auto-fills; second symbol missing close → post-fill terminal status.
	env := setupRunnerEnv(t, stubResponses{
		data: `{"bars":[{"symbol":"AAPL","trade_date":"2026-07-22","open":190,"high":192,"low":188,"close":191,"volume":1000000}],"warnings":[]}`,
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: `{"proposals":[{"symbol":"AAPL","side":"buy","qty":10,"estimated_notional":1910,"estimated_cash_impact":-1910},{"symbol":"MSFT","side":"buy","qty":5,"estimated_notional":1000,"estimated_cash_impact":-1000}],"warnings":[]}`,
		risk:      `{"items":[],"warnings":[]}`,
	})
	// MSFT must be holdable so the failure path is missing close, not not_holdable.
	if err := env.db.Create(&models.WatchlistSymbol{Symbol: "MSFT", CanHold: true}).Error; err != nil {
		t.Fatalf("seed MSFT: %v", err)
	}

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
	if err == nil {
		t.Fatal("expected mid-fill error")
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed (not stuck at risk)", run.Status)
	}
	if run.ErrorMsg == "" {
		t.Fatal("expected error_msg")
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 1 {
		t.Fatalf("orders: got %d want 1 (first fill kept)", orders)
	}

	var pendingAuto int64
	if err := env.db.Model(&models.TradeProposal{}).
		Where("status = ?", workflow.ProposalPendingAuto).Count(&pendingAuto).Error; err != nil {
		t.Fatalf("pending_auto: %v", err)
	}
	if pendingAuto != 0 {
		t.Fatalf("pending_auto orphans: got %d want 0", pendingAuto)
	}

	var cancelled int64
	if err := env.db.Model(&models.TradeProposal{}).
		Where("status = ?", workflow.ProposalCancelled).Count(&cancelled).Error; err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if cancelled != 1 {
		t.Fatalf("cancelled proposals: got %d want 1", cancelled)
	}
}

func TestRunEODAllowsSecondRunSameTradeDate(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})

	id1, err := env.runner.RunEOD(context.Background(), workflow.RunParams{
		TradeDate: tradeDate,
		Trigger:   workflow.TriggerPreOpen,
	})
	if err != nil {
		t.Fatalf("first RunEOD: %v", err)
	}
	id2, err := env.runner.RunEOD(context.Background(), workflow.RunParams{
		TradeDate: tradeDate,
		Trigger:   workflow.TriggerIntraday,
	})
	if err != nil {
		t.Fatalf("second RunEOD: %v", err)
	}
	if id1 == 0 || id2 == 0 || id1 == id2 {
		t.Fatalf("expected two distinct run IDs, got %d and %d", id1, id2)
	}

	var runs int64
	if err := env.db.Model(&models.WorkflowRun{}).Count(&runs).Error; err != nil {
		t.Fatalf("runs: %v", err)
	}
	if runs != 2 {
		t.Fatalf("runs: got %d want 2", runs)
	}
}

func TestRunEODInvalidPortfolioSchemaNoFills(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: `{"warnings":[]}`, // missing proposals
		risk:      `{"items":[],"warnings":[]}`,
	})

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
	if err == nil {
		t.Fatal("expected schema validation error")
	}
	if !errors.Is(err, workflow.ErrInvalidPortfolioSchema) {
		t.Fatalf("error: got %v want ErrInvalidPortfolioSchema", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed", run.Status)
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
}

func TestRunEODNAVFailurePreservesExecutedStatus(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})
	inner := env.runner.Ledger.(*ledger.Service)
	env.runner.Ledger = &failingNAVLedger{Service: inner, err: errors.New("nav boom")}

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
	if err == nil {
		t.Fatal("expected UpsertNAV error")
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed (must not flip to failed)", run.Status)
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 1 {
		t.Fatalf("orders: got %d want 1 (fill must stick)", orders)
	}
}

type failingNAVLedger struct {
	*ledger.Service
	err error
}

func (f *failingNAVLedger) UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error) {
	return models.NavSnapshot{}, f.err
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

func portfolioSellJSON(qty, notional, cashImpact float64) string {
	return fmt.Sprintf(
		`{"proposals":[{"symbol":"AAPL","side":"sell","qty":%v,"estimated_notional":%v,"estimated_cash_impact":%v}],"warnings":[]}`,
		qty, notional, cashImpact,
	)
}

func TestRunEODBuyRejectedWhenNotHoldable(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		risk:      `{"items":[{"symbol":"AAPL","side":"buy","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})
	if err := env.db.Model(&models.WatchlistSymbol{}).Where("symbol = ?", "AAPL").Update("can_hold", false).Error; err != nil {
		t.Fatalf("set can_hold: %v", err)
	}

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalRejected {
		t.Fatalf("proposal: got %+v", proposals)
	}
	if !strings.Contains(proposals[0].BreachReasonsJSON, "not_holdable") {
		t.Fatalf("breach_reasons_json: got %q want not_holdable", proposals[0].BreachReasonsJSON)
	}

	var orders int64
	if err := env.db.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}
}

func TestRunEODSellAllowedWhenNotHoldable(t *testing.T) {
	env := setupRunnerEnv(t, stubResponses{
		data:      dataBarsJSON(191),
		research:  `{"items":[{"symbol":"AAPL","bias":"bear","confidence":0.7,"thesis":"ok"}],"warnings":[]}`,
		decision:  `{"intents":[{"symbol":"AAPL","side":"sell","urgency":"normal","rationale":"ok"}],"warnings":[]}`,
		portfolio: portfolioSellJSON(5, 955, 955),
		risk:      `{"items":[{"symbol":"AAPL","side":"sell","flags":["size_ok"],"scores":{"liquidity":0.9},"suggested_action":"auto"}],"warnings":[]}`,
	})
	if err := env.db.Model(&models.WatchlistSymbol{}).Where("symbol = ?", "AAPL").Update("can_hold", false).Error; err != nil {
		t.Fatalf("set can_hold: %v", err)
	}
	var account models.Account
	if err := env.db.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if err := env.db.Create(&models.Position{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Qty:       10,
		AvgCost:   180,
	}).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}

	runID, err := env.runner.RunEOD(context.Background(), eodParams(tradeDate))
	if err != nil {
		t.Fatalf("RunEOD: %v", err)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: got %+v want filled", proposals)
	}
	if strings.Contains(proposals[0].BreachReasonsJSON, "not_holdable") {
		t.Fatalf("sell should not be rejected as not_holdable: %q", proposals[0].BreachReasonsJSON)
	}
}
