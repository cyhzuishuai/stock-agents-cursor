package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type fakeBroker struct {
	mu      sync.Mutex
	submit  func(broker.OrderRequest) (broker.Order, error)
	get     map[string]broker.Order
	getErr  error
	acct    broker.Account
	pos     []broker.Position
	acctErr         error
	acctFailAfter   int // GetAccount succeeds this many times, then returns acctErr
	getAccountCalls int

	submitCalls []broker.OrderRequest
	nextID      int
}

func (f *fakeBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acctFailAfter > 0 {
		f.getAccountCalls++
		if f.getAccountCalls > f.acctFailAfter {
			if f.acctErr != nil {
				return broker.Account{}, f.acctErr
			}
			return broker.Account{}, errors.New("account unavailable")
		}
		return f.acct, nil
	}
	if f.acctErr != nil {
		return broker.Account{}, f.acctErr
	}
	return f.acct, nil
}

func (f *fakeBroker) ListPositions(ctx context.Context) ([]broker.Position, error) {
	return f.pos, nil
}

func (f *fakeBroker) SubmitOrder(ctx context.Context, req broker.OrderRequest) (broker.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls = append(f.submitCalls, req)
	if f.submit != nil {
		return f.submit(req)
	}
	f.nextID++
	id := fmt.Sprintf("brk-%d", f.nextID)
	o := broker.Order{
		ID:             id,
		ClientOrderID:  req.ClientOrderID,
		Symbol:         req.Symbol,
		Side:           req.Side,
		Qty:            req.Qty,
		FilledQty:      req.Qty,
		FilledAvgPrice: 191,
		Status:         "filled",
	}
	if f.get == nil {
		f.get = map[string]broker.Order{}
	}
	f.get[id] = o
	return o, nil
}

func (f *fakeBroker) GetOrder(ctx context.Context, brokerOrderID string) (broker.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return broker.Order{}, f.getErr
	}
	if o, ok := f.get[brokerOrderID]; ok {
		return o, nil
	}
	return broker.Order{}, fmt.Errorf("order not found: %s", brokerOrderID)
}

func (f *fakeBroker) ListOrders(ctx context.Context, status string) ([]broker.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]broker.Order, 0, len(f.get))
	for _, o := range f.get {
		out = append(out, o)
	}
	return out, nil
}

func (f *fakeBroker) submitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submitCalls)
}

const tradeDate = "2026-07-22"

func workflowParams(td string) workflow.RunParams {
	return workflow.RunParams{TradeDate: td}
}

func TestRunWorkflowHappyPathAutoExecBuy(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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
	if len(steps) != 2 {
		t.Fatalf("steps count: got %d want 2", len(steps))
	}
	wantSteps := []string{
		workflow.StepAnalyst, workflow.StepPortfolio,
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
	// Local ledger cash is unchanged — Alpaca Paper is fill authority.
	if account.Cash != 100000 {
		t.Fatalf("cash: got %v want %v (ledger must not ApplyFill)", account.Cash, 100000)
	}
	if env.broker.submitCount() != 1 {
		t.Fatalf("SubmitOrder calls: got %d want 1", env.broker.submitCount())
	}
	if orders[0].BrokerOrderID == "" || orders[0].ClientOrderID == "" {
		t.Fatalf("order mirror missing broker ids: %+v", orders[0])
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

func TestRunWorkflowAgentFailureMidChainNoFills(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
		failAt:    workflow.StepPortfolio,
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
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

func TestRunWorkflowBreachCreatesPendingApprovalNoFill(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000,
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeRequireApproval,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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

func TestRunWorkflowUnderstatedNotionalStillBreachesMaxOrder(t *testing.T) {
	// Agent understates notional (100) but qty*broker_mark = 100*191 = 19100 > max 10000.
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 100, -100),
	})
	env.broker.pos = []broker.Position{{Symbol: "AAPL", Qty: 0, CurrentPrice: 191}}

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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

func TestRunWorkflowResolvesExecutionModeFromStrategyDB(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000,
	})

	st := models.Strategy{
		Name:          "auto-reject-test",
		IsActive:      true,
		ExecutionMode: workflow.ExecutionModeAutoReject,
	}
	if err := env.db.Create(&st).Error; err != nil {
		t.Fatalf("create strategy: %v", err)
	}

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:  tradeDate,
		StrategyID: &st.ID,
		Trigger:    workflow.TriggerManual,
		// ExecutionMode intentionally empty — runner must load from DB.
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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

func TestRunWorkflowAutoRejectBreachesRejectsProposalNoApproval(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100), // exceeds max_order_notional 10000,
	})

	sid := uint(1)
	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		StrategyID:    &sid,
		Trigger:       workflow.TriggerManual,
		ExecutionMode: workflow.ExecutionModeAutoReject,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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

func TestRunWorkflowMidFillFailureSetsTerminalFailed(t *testing.T) {
	// First symbol auto-fills; second symbol missing mark (no broker price, zero notional).
	env := setupRunnerEnv(t, &stubResponses{
		analyst: analystResultJSON(),
		portfolio: envelopeJSON(`{"proposals":[{"symbol":"AAPL","side":"buy","qty":10,"estimated_notional":1910,"estimated_cash_impact":-1910},{"symbol":"MSFT","side":"buy","qty":5,"estimated_notional":0,"estimated_cash_impact":0}],"warnings":[]}`),
	})
	// MSFT must be holdable so the failure path is missing mark, not not_holdable.
	if err := env.db.Create(&models.WatchlistSymbol{Symbol: "MSFT", CanHold: true}).Error; err != nil {
		t.Fatalf("seed MSFT: %v", err)
	}

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
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

func TestRunWorkflowAllowsSecondRunSameTradeDate(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})

	id1, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate: tradeDate,
		Trigger:   workflow.TriggerPreOpen,
	})
	if err != nil {
		t.Fatalf("first RunWorkflow: %v", err)
	}
	id2, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate: tradeDate,
		Trigger:   workflow.TriggerIntraday,
	})
	if err != nil {
		t.Fatalf("second RunWorkflow: %v", err)
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

func TestRunWorkflowInvalidPortfolioSchemaNoFills(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: envelopeJSON(`{"warnings":[]}`), // missing proposals,
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
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

func TestRunWorkflowNAVFailurePreservesExecutedStatus(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})
	inner := env.runner.Ledger.(*ledger.Service)
	env.runner.Ledger = &failingNAVLedger{Service: inner, err: errors.New("nav boom")}
	// Broker submit still works; GetAccount fails on upsertNAV so it falls through to ledger.
	env.broker.acctErr = errors.New("account unavailable")
	env.broker.acctFailAfter = 3 // snapshot + portfolioState before/after fill; upsertNAV fails

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
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
	analyst, portfolio string
	failAt             string
	lastPortfolioPrior map[string]any // filled when agent=portfolio

	// HITL / resume stubs
	resumeAnalyst      string
	resumePortfolio    string
	runCallCounts      map[string]int
	resumeCallCounts   map[string]int
	lastRunThreadID    string
	lastResumeThreadID string
	lastResumeBody     map[string]any
}

type runnerEnv struct {
	db     *gorm.DB
	runner *workflow.Runner
	broker *fakeBroker
}

func setupRunnerEnv(t *testing.T, stubs *stubResponses) *runnerEnv {
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

	runtimeURL := startAgentRuntimeStub(t, stubs)

	fb := &fakeBroker{
		acct: broker.Account{ID: "paper", Cash: 100000, Equity: 100000, PortfolioValue: 100000},
		get:  map[string]broker.Order{},
	}
	runner := &workflow.Runner{
		DB: gormDB,
		Agents: &agentsclient.Client{
			RuntimeURL: runtimeURL,
			MaxRetries: 0,
		},
		Ledger: &ledger.Service{DB: gormDB},
		Risk: risk.LoadEngineFromMap(map[string]float64{
			"max_order_notional":     10000,
			"max_single_name_weight": 0.20,
			"min_cash_ratio":         0.10,
		}),
		Redis:  rdb,
		Broker: fb,
		Config: cfg,
	}
	return &runnerEnv{db: gormDB, runner: runner, broker: fb}
}

func startAgentRuntimeStub(t *testing.T, stubs *stubResponses) string {
	t.Helper()
	if stubs.runCallCounts == nil {
		stubs.runCallCounts = map[string]int{}
	}
	if stubs.resumeCallCounts == nil {
		stubs.resumeCallCounts = map[string]int{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/run":
			var req struct {
				Agent            string         `json:"agent"`
				ThreadID         string         `json:"thread_id"`
				PriorStepOutputs map[string]any `json:"prior_step_outputs"`
			}
			dec := json.NewDecoder(r.Body)
			if err := dec.Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			stubs.runCallCounts[req.Agent]++
			stubs.lastRunThreadID = req.ThreadID
			var body string
			switch req.Agent {
			case workflow.StepAnalyst:
				body = stubs.analyst
			case workflow.StepPortfolio:
				stubs.lastPortfolioPrior = req.PriorStepOutputs
				body = stubs.portfolio
			default:
				http.Error(w, "unknown agent", http.StatusBadRequest)
				return
			}
			if stubs.failAt == req.Agent {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			if body == "" {
				http.Error(w, "empty stub", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case "/v1/resume":
			var req struct {
				ThreadID       string         `json:"thread_id"`
				HumanResponse  map[string]any `json:"human_response"`
			}
			dec := json.NewDecoder(r.Body)
			if err := dec.Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			stubs.lastResumeThreadID = req.ThreadID
			stubs.lastResumeBody = map[string]any{
				"thread_id":      req.ThreadID,
				"human_response": req.HumanResponse,
			}
			agent := req.ThreadID
			if i := strings.LastIndex(req.ThreadID, ":"); i >= 0 {
				agent = req.ThreadID[i+1:]
			}
			stubs.resumeCallCounts[agent]++
			var body string
			switch agent {
			case workflow.StepAnalyst:
				body = stubs.resumeAnalyst
			case workflow.StepPortfolio:
				body = stubs.resumePortfolio
			default:
				http.Error(w, "unknown resume agent", http.StatusBadRequest)
				return
			}
			if body == "" {
				http.Error(w, "empty resume stub", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func interruptedEnvelopeJSON(threadID, question string) string {
	return fmt.Sprintf(
		`{"status":"interrupted","thread_id":%q,"human_request":{"question":%q},"trace":{"agent":"test","stop_reason":"interrupted"}}`,
		threadID, question,
	)
}

func envelopeJSON(result string) string {
	return fmt.Sprintf(`{"result":%s,"trace":{"agent":"test","rounds":[],"stop_reason":"final"}}`, result)
}

func analystResultJSON() string {
	return envelopeJSON(`{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]}`)
}

func analystEnvelopeWithHandoffJSON() string {
	return `{
	  "result":{"items":[{"symbol":"AAPL","bias":"bull","confidence":0.7,"thesis":"ok","side":"buy","urgency":"normal","rationale":"ok"}],"warnings":[]},
	  "handoff":{"thesis_by_symbol":{"AAPL":{"summary":"ok","bias":"bull","confidence":0.7}},"evidence_refs":["get_bars:AAPL"],"open_questions":["volume?"]},
	  "working_memory":{"notes":["n1"],"evidence_refs":["get_bars:AAPL"],"open_questions":["volume?"]},
	  "trace":{"agent":"analyst","rounds":[],"stop_reason":"final"}
	}`
}

func TestRunWorkflowInjectsAnalystHandoffIntoPortfolioPrior(t *testing.T) {
	stubs := stubResponses{
		analyst:   analystEnvelopeWithHandoffJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, &stubs)
	_, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	prior := stubs.lastPortfolioPrior
	if prior == nil {
		t.Fatal("expected portfolio prior captured")
	}
	analyst, ok := prior["analyst"].(map[string]any)
	if !ok || analyst["items"] == nil {
		t.Fatalf("analyst must remain result-shaped with items: %#v", prior["analyst"])
	}
	if _, ok := prior["analyst_handoff"]; !ok {
		t.Fatalf("missing analyst_handoff: %#v", prior)
	}
	if _, ok := prior["analyst_working_memory"]; !ok {
		t.Fatalf("missing analyst_working_memory: %#v", prior)
	}
}

func TestRunWorkflowWithoutHandoffStillSucceeds(t *testing.T) {
	stubs := stubResponses{
		analyst:   analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, &stubs)
	_, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	prior := stubs.lastPortfolioPrior
	if _, ok := prior["analyst_handoff"]; ok {
		t.Fatalf("did not expect analyst_handoff: %#v", prior)
	}
}

func portfolioBuyJSON(qty, notional, cashImpact float64) string {
	return envelopeJSON(fmt.Sprintf(
		`{"proposals":[{"symbol":"AAPL","side":"buy","qty":%v,"estimated_notional":%v,"estimated_cash_impact":%v}],"warnings":[]}`,
		qty, notional, cashImpact,
	))
}

func portfolioSellJSON(qty, notional, cashImpact float64) string {
	return envelopeJSON(fmt.Sprintf(
		`{"proposals":[{"symbol":"AAPL","side":"sell","qty":%v,"estimated_notional":%v,"estimated_cash_impact":%v}],"warnings":[]}`,
		qty, notional, cashImpact,
	))
}

func TestRunWorkflowBuyRejectedWhenNotHoldable(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})
	if err := env.db.Model(&models.WatchlistSymbol{}).Where("symbol = ?", "AAPL").Update("can_hold", false).Error; err != nil {
		t.Fatalf("set can_hold: %v", err)
	}

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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
	if env.broker.submitCount() != 0 {
		t.Fatalf("SubmitOrder calls: got %d want 0", env.broker.submitCount())
	}
}

func TestRunWorkflowSellAllowedWhenNotHoldable(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioSellJSON(5, 955, 955),
	})
	if err := env.db.Model(&models.WatchlistSymbol{}).Where("symbol = ?", "AAPL").Update("can_hold", false).Error; err != nil {
		t.Fatalf("set can_hold: %v", err)
	}
	// Broker is fill authority — seed position on fake broker, not local ledger ApplyFill.
	env.broker.pos = []broker.Position{{Symbol: "AAPL", Qty: 10, AvgCost: 180, CurrentPrice: 191}}
	env.broker.acct = broker.Account{ID: "paper", Cash: 80890, Equity: 100000, PortfolioValue: 100000}

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
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
	if env.broker.submitCount() != 1 {
		t.Fatalf("SubmitOrder calls: got %d want 1", env.broker.submitCount())
	}

	var account models.Account
	if err := env.db.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash: got %v want %v (ledger must not ApplyFill)", account.Cash, 100000)
	}
}

func TestRunWorkflowBypassRiskSubmitsWithoutRisk(t *testing.T) {
	// qty*close = 100*191 = 19100 > max_order_notional 10000 — would always breach.
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100),
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeBypassRisk,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	if env.broker.submitCount() != 1 {
		t.Fatalf("SubmitOrder calls: got %d want 1", env.broker.submitCount())
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: %+v", proposals)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}
}

func TestRunWorkflowAutoRejectStillRejects(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100),
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeAutoReject,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	if env.broker.submitCount() != 0 {
		t.Fatalf("SubmitOrder calls: got %d want 0", env.broker.submitCount())
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalRejected {
		t.Fatalf("proposal: %+v", proposals)
	}
}

func TestRunWorkflowRequireApprovalDoesNotSubmit(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(100, 19100, -19100),
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeRequireApproval,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	if env.broker.submitCount() != 0 {
		t.Fatalf("SubmitOrder calls: got %d want 0", env.broker.submitCount())
	}

	var approvals []models.Approval
	if err := env.db.Find(&approvals).Error; err != nil {
		t.Fatalf("approvals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Status != workflow.ApprovalPending {
		t.Fatalf("approval: %+v", approvals)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusAwaitingApproval {
		t.Fatalf("run status: got %q want awaiting_approval", run.Status)
	}
}

func TestRunWorkflowPassSubmitsToBroker(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeRequireApproval,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	if env.broker.submitCount() != 1 {
		t.Fatalf("SubmitOrder calls: got %d want 1", env.broker.submitCount())
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: %+v", proposals)
	}

	var orders []models.Order
	if err := env.db.Find(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: got %d want 1", len(orders))
	}
	if orders[0].BrokerOrderID == "" || orders[0].FillPrice != 191 || orders[0].Status != "filled" {
		t.Fatalf("order mirror: %+v", orders[0])
	}
}

func TestRunWorkflowNilBrokerWithSubmitFailsRun(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})
	env.runner.Broker = nil

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeBypassRisk,
	})
	if err == nil {
		t.Fatal("expected error when broker is nil")
	}
	if !strings.Contains(err.Error(), "broker is required") {
		t.Fatalf("error: got %v want broker is required (agent snapshot)", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed (not executed)", run.Status)
	}

	var proposals int64
	if err := env.db.Model(&models.TradeProposal{}).Count(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if proposals != 0 {
		t.Fatalf("proposals: got %d want 0 (fail before agent chain)", proposals)
	}
}

func TestRunWorkflowSubmitOrderErrorRejectsAndContinues(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: envelopeJSON(`{"proposals":[{"symbol":"AAPL","side":"buy","qty":5,"estimated_notional":955,"estimated_cash_impact":-955},{"symbol":"AAPL","side":"buy","qty":5,"estimated_notional":955,"estimated_cash_impact":-955}],"warnings":[]}`),
	})

	var submitN int
	env.broker.submit = func(req broker.OrderRequest) (broker.Order, error) {
		submitN++
		if submitN == 1 {
			return broker.Order{}, errors.New("insufficient buying power")
		}
		// Called under fakeBroker.mu — do not re-lock.
		env.broker.nextID++
		id := fmt.Sprintf("brk-%d", env.broker.nextID)
		o := broker.Order{
			ID:             id,
			ClientOrderID:  req.ClientOrderID,
			Symbol:         req.Symbol,
			Side:           req.Side,
			Qty:            req.Qty,
			FilledQty:      req.Qty,
			FilledAvgPrice: 191,
			Status:         "filled",
		}
		if env.broker.get == nil {
			env.broker.get = map[string]broker.Order{}
		}
		env.broker.get[id] = o
		return o, nil
	}

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeBypassRisk,
	})
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if env.broker.submitCount() != 2 {
		t.Fatalf("SubmitOrder calls: got %d want 2", env.broker.submitCount())
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Order("id").Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 2 {
		t.Fatalf("proposals: got %d want 2", len(proposals))
	}
	if proposals[0].Status != workflow.ProposalRejected {
		t.Fatalf("first proposal: got %s want rejected", proposals[0].Status)
	}
	if !strings.Contains(proposals[0].BreachReasonsJSON, "broker:") {
		t.Fatalf("first proposal reasons: %s", proposals[0].BreachReasonsJSON)
	}
	if proposals[1].Status != workflow.ProposalFilled {
		t.Fatalf("second proposal: got %s want filled", proposals[1].Status)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}
}

func TestRunWorkflowPostSubmitGetOrderErrorKeepsSubmitted(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:  analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})
	env.broker.getErr = errors.New("alpaca get order unavailable")

	runID, err := env.runner.RunWorkflow(context.Background(), workflow.RunParams{
		TradeDate:     tradeDate,
		ExecutionMode: workflow.ExecutionModeBypassRisk,
	})
	if err == nil {
		t.Fatal("expected error from persistent GetOrder failure")
	}
	if env.broker.submitCount() != 1 {
		t.Fatalf("SubmitOrder calls: got %d want 1", env.broker.submitCount())
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("proposals: got %d want 1", len(proposals))
	}
	if proposals[0].Status != workflow.ProposalSubmitted {
		t.Fatalf("proposal status: got %q want submitted (must not flip to rejected)", proposals[0].Status)
	}
	if proposals[0].BreachReasonsJSON != "" && strings.Contains(proposals[0].BreachReasonsJSON, "broker:") {
		t.Fatalf("must not store SubmitOrder-style reject reason: %s", proposals[0].BreachReasonsJSON)
	}

	var orders []models.Order
	if err := env.db.Find(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if len(orders) != 1 || orders[0].Status != "submitted" {
		t.Fatalf("order mirror: %+v", orders)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed", run.Status)
	}
}

func TestRunWorkflowInterruptedAnalystStopsChain(t *testing.T) {
	stubs := &stubResponses{
		analyst:   interruptedEnvelopeJSON("placeholder", "confirm thesis?"),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusAwaitingAgentInput {
		t.Fatalf("run status: got %q want %s", run.Status, workflow.StatusAwaitingAgentInput)
	}
	if run.Status == workflow.StatusAwaitingApproval {
		t.Fatal("must not use awaiting_approval for agent HITL")
	}

	wantThread := fmt.Sprintf("%d:%s", runID, workflow.StepAnalyst)
	if stubs.lastRunThreadID != wantThread {
		t.Fatalf("thread_id: got %q want %q", stubs.lastRunThreadID, wantThread)
	}
	if stubs.runCallCounts[workflow.StepPortfolio] != 0 {
		t.Fatalf("portfolio calls: got %d want 0", stubs.runCallCounts[workflow.StepPortfolio])
	}

	var steps []models.WorkflowStepResult
	if err := env.db.Where("run_id = ?", runID).Order("id").Find(&steps).Error; err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps count: got %d want 1", len(steps))
	}
	if steps[0].Step != workflow.StepAnalyst || steps[0].Status != workflow.StepStatusInterrupted {
		t.Fatalf("step: got %s/%s want analyst/interrupted", steps[0].Step, steps[0].Status)
	}

	var proposals int64
	if err := env.db.Model(&models.TradeProposal{}).Where("run_id = ?", runID).Count(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if proposals != 0 {
		t.Fatalf("proposals: got %d want 0", proposals)
	}
}

func TestResumeAgentContinuesChainToExecuted(t *testing.T) {
	stubs := &stubResponses{
		analyst:       interruptedEnvelopeJSON("placeholder", "confirm thesis?"),
		resumeAnalyst: analystResultJSON(),
		portfolio:     portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	status, err := env.runner.ResumeAgent(context.Background(), runID, workflow.StepAnalyst, json.RawMessage(`{"text":"yes"}`))
	if err != nil {
		t.Fatalf("ResumeAgent: %v", err)
	}
	if status != workflow.StatusExecuted {
		t.Fatalf("ResumeAgent status: got %q want executed", status)
	}

	wantThread := fmt.Sprintf("%d:%s", runID, workflow.StepAnalyst)
	if stubs.lastResumeThreadID != wantThread {
		t.Fatalf("resume thread_id: got %q want %q", stubs.lastResumeThreadID, wantThread)
	}
	if stubs.resumeCallCounts[workflow.StepAnalyst] != 1 {
		t.Fatalf("resume calls: got %d want 1", stubs.resumeCallCounts[workflow.StepAnalyst])
	}
	if stubs.runCallCounts[workflow.StepPortfolio] != 1 {
		t.Fatalf("portfolio calls after resume: got %d want 1", stubs.runCallCounts[workflow.StepPortfolio])
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var steps []models.WorkflowStepResult
	if err := env.db.Where("run_id = ?", runID).Order("id").Find(&steps).Error; err != nil {
		t.Fatalf("steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps count: got %d want 2", len(steps))
	}
	if steps[0].Step != workflow.StepAnalyst || steps[0].Status != workflow.StepStatusOK {
		t.Fatalf("analyst step: got %s/%s want ok", steps[0].Step, steps[0].Status)
	}
	if steps[1].Step != workflow.StepPortfolio || steps[1].Status != workflow.StepStatusOK {
		t.Fatalf("portfolio step: got %s/%s want ok", steps[1].Step, steps[1].Status)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: got %+v", proposals)
	}
}

func TestResumeAgentConflictWhenNotAwaiting(t *testing.T) {
	env := setupRunnerEnv(t, &stubResponses{
		analyst:   analystResultJSON(),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	})
	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	_, err = env.runner.ResumeAgent(context.Background(), runID, workflow.StepAnalyst, json.RawMessage(`{"text":"x"}`))
	if !errors.Is(err, workflow.ErrRunNotAwaitingAgentInput) {
		t.Fatalf("ResumeAgent: got %v want ErrRunNotAwaitingAgentInput", err)
	}
}

func TestResumeAgentInterruptedAgainKeepsAwaiting(t *testing.T) {
	stubs := &stubResponses{
		analyst:       interruptedEnvelopeJSON("placeholder", "q1"),
		resumeAnalyst: interruptedEnvelopeJSON("placeholder", "q2"),
		portfolio:     portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	status, err := env.runner.ResumeAgent(context.Background(), runID, workflow.StepAnalyst, json.RawMessage(`{"text":"more"}`))
	if err != nil {
		t.Fatalf("ResumeAgent: %v", err)
	}
	if status != workflow.StatusAwaitingAgentInput {
		t.Fatalf("ResumeAgent status: got %q want awaiting_agent_input", status)
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusAwaitingAgentInput {
		t.Fatalf("run status: got %q want %s", run.Status, workflow.StatusAwaitingAgentInput)
	}
	if stubs.runCallCounts[workflow.StepPortfolio] != 0 {
		t.Fatalf("portfolio must not run: got %d", stubs.runCallCounts[workflow.StepPortfolio])
	}

	var step models.WorkflowStepResult
	if err := env.db.Where("run_id = ? AND step = ?", runID, workflow.StepAnalyst).First(&step).Error; err != nil {
		t.Fatalf("step: %v", err)
	}
	if step.Status != workflow.StepStatusInterrupted {
		t.Fatalf("step status: got %q want interrupted", step.Status)
	}
	if !strings.Contains(step.PayloadJSON, `"q2"`) {
		t.Fatalf("payload should refresh human_request: %s", step.PayloadJSON)
	}
}

func TestResumeAgentPortfolioLastAgentToExecuted(t *testing.T) {
	stubs := &stubResponses{
		analyst:         analystResultJSON(),
		portfolio:       interruptedEnvelopeJSON("placeholder", "size ok?"),
		resumePortfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusAwaitingAgentInput {
		t.Fatalf("run status: got %q want awaiting_agent_input", run.Status)
	}

	status, err := env.runner.ResumeAgent(context.Background(), runID, workflow.StepPortfolio, json.RawMessage(`{"text":"go"}`))
	if err != nil {
		t.Fatalf("ResumeAgent: %v", err)
	}
	if status != workflow.StatusExecuted {
		t.Fatalf("ResumeAgent status: got %q want executed", status)
	}
	if stubs.resumeCallCounts[workflow.StepPortfolio] != 1 {
		t.Fatalf("portfolio resume calls: got %d want 1", stubs.resumeCallCounts[workflow.StepPortfolio])
	}

	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var step models.WorkflowStepResult
	if err := env.db.Where("run_id = ? AND step = ?", runID, workflow.StepPortfolio).First(&step).Error; err != nil {
		t.Fatalf("portfolio step: %v", err)
	}
	if step.Status != workflow.StepStatusOK {
		t.Fatalf("portfolio step status: got %q want ok", step.Status)
	}

	var proposals []models.TradeProposal
	if err := env.db.Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if len(proposals) != 1 || proposals[0].Status != workflow.ProposalFilled {
		t.Fatalf("proposal: got %+v", proposals)
	}
}

func TestResumeAgentLastAgentPostFillFailureMarksFailed(t *testing.T) {
	stubs := &stubResponses{
		analyst:         analystResultJSON(),
		portfolio:       interruptedEnvelopeJSON("placeholder", "size?"),
		resumePortfolio: envelopeJSON(`{"warnings":[]}`), // missing proposals → schema fail after step ok
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}

	_, err = env.runner.ResumeAgent(context.Background(), runID, workflow.StepPortfolio, json.RawMessage(`{"text":"go"}`))
	if err == nil {
		t.Fatal("expected ResumeAgent error from invalid portfolio schema")
	}

	var run models.WorkflowRun
	if err := env.db.First(&run, runID).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.Status != workflow.StatusFailed {
		t.Fatalf("run status: got %q want failed (must not stick on awaiting_agent_input)", run.Status)
	}

	var step models.WorkflowStepResult
	if err := env.db.Where("run_id = ? AND step = ?", runID, workflow.StepPortfolio).First(&step).Error; err != nil {
		t.Fatalf("portfolio step: %v", err)
	}
	if step.Status != workflow.StepStatusOK {
		t.Fatalf("portfolio step should stay ok after resume: got %q", step.Status)
	}
}

func TestResumeAgentWrongAgentNoInterruptedStep(t *testing.T) {
	stubs := &stubResponses{
		analyst:   interruptedEnvelopeJSON("placeholder", "q"),
		portfolio: portfolioBuyJSON(10, 1910, -1910),
	}
	env := setupRunnerEnv(t, stubs)

	runID, err := env.runner.RunWorkflow(context.Background(), workflowParams(tradeDate))
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	_, err = env.runner.ResumeAgent(context.Background(), runID, workflow.StepPortfolio, json.RawMessage(`{"text":"x"}`))
	if !errors.Is(err, workflow.ErrNoInterruptedStep) {
		t.Fatalf("ResumeAgent: got %v want ErrNoInterruptedStep", err)
	}
	_, err = env.runner.ResumeAgent(context.Background(), runID, "research", json.RawMessage(`{"text":"x"}`))
	if !errors.Is(err, workflow.ErrUnknownAgent) {
		t.Fatalf("ResumeAgent: got %v want ErrUnknownAgent", err)
	}
}
