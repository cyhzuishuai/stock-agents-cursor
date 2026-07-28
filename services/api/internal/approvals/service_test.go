package approvals_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"gorm.io/gorm"
)

const tradeDate = "2026-07-22"

type fakeBroker struct {
	mu          sync.Mutex
	submitCalls []broker.OrderRequest
	submitErr   error
	get         map[string]broker.Order
	nextID      int
	acct        broker.Account
}

func (f *fakeBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	return f.acct, nil
}

func (f *fakeBroker) ListPositions(ctx context.Context) ([]broker.Position, error) {
	return nil, nil
}

func (f *fakeBroker) SubmitOrder(ctx context.Context, req broker.OrderRequest) (broker.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls = append(f.submitCalls, req)
	if f.submitErr != nil {
		return broker.Order{}, f.submitErr
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
	if o, ok := f.get[brokerOrderID]; ok {
		return o, nil
	}
	return broker.Order{}, fmt.Errorf("order not found: %s", brokerOrderID)
}

func (f *fakeBroker) ListOrders(ctx context.Context, status string) ([]broker.Order, error) {
	return nil, nil
}

func TestDecideApprovedSubmitsToBroker(t *testing.T) {
	fb := &fakeBroker{acct: broker.Account{Cash: 100000, Equity: 100000}}
	svc, gormDB, runID, approvalID := setupPendingApproval(t, fb)

	var accountBefore models.Account
	if err := gormDB.First(&accountBefore).Error; err != nil {
		t.Fatalf("account before: %v", err)
	}

	if err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalApproved || approval.Note != "ok" || *approval.DecidedBy != 1 {
		t.Fatalf("approval: %+v", approval)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalFilled {
		t.Fatalf("proposal status: got %q want filled", proposal.Status)
	}

	var orders []models.Order
	if err := gormDB.Find(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders: got %d want 1 (%+v)", len(orders), orders)
	}
	if orders[0].BrokerOrderID == "" {
		t.Fatalf("expected BrokerOrderID on mirror order: %+v", orders[0])
	}
	if orders[0].FillPrice != 191 || orders[0].Qty != 100 || orders[0].Status != "filled" {
		t.Fatalf("orders: %+v", orders)
	}

	var run models.WorkflowRun
	if err := gormDB.First(&run, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var accountAfter models.Account
	if err := gormDB.First(&accountAfter).Error; err != nil {
		t.Fatalf("account after: %v", err)
	}
	if accountAfter.Cash != accountBefore.Cash {
		t.Fatalf("ledger cash changed on approve: before %v after %v", accountBefore.Cash, accountAfter.Cash)
	}

	var nav models.NavSnapshot
	if err := gormDB.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Nav != 100000 {
		t.Fatalf("nav: got %v want 100000", nav.Nav)
	}
}

func TestDecideApprovedFillsAndExecutesRun(t *testing.T) {
	fb := &fakeBroker{acct: broker.Account{Cash: 100000, Equity: 100000}}
	svc, gormDB, runID, approvalID := setupPendingApproval(t, fb)

	if err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalApproved || approval.Note != "ok" || *approval.DecidedBy != 1 {
		t.Fatalf("approval: %+v", approval)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalFilled {
		t.Fatalf("proposal status: got %q want filled", proposal.Status)
	}

	var orders []models.Order
	if err := gormDB.Find(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if len(orders) != 1 || orders[0].BrokerOrderID == "" || orders[0].FillPrice != 191 || orders[0].Qty != 100 {
		t.Fatalf("orders: %+v", orders)
	}

	var run models.WorkflowRun
	if err := gormDB.First(&run, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash: got %v want %v (no local ApplyFill)", account.Cash, 100000)
	}

	var nav models.NavSnapshot
	if err := gormDB.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Nav != 100000 {
		t.Fatalf("nav: got %v want 100000", nav.Nav)
	}
}

func TestDecideApprovedNilBrokerReturnsErrBrokerNotConfigured(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t, nil)

	err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1)
	if !errors.Is(err, workflow.ErrBrokerNotConfigured) {
		t.Fatalf("Decide: got %v want ErrBrokerNotConfigured", err)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalPending {
		t.Fatalf("approval status: got %q want pending", approval.Status)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalAwaitingApproval {
		t.Fatalf("proposal status: got %q want awaiting_approval", proposal.Status)
	}

	var orders int64
	if err := gormDB.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}
}

func TestDecideApprovedSubmitOrderErrorRejectsProposal(t *testing.T) {
	submitErr := errors.New("insufficient buying power")
	fb := &fakeBroker{
		acct:      broker.Account{Cash: 100000, Equity: 100000},
		submitErr: submitErr,
	}
	svc, gormDB, runID, approvalID := setupPendingApproval(t, fb)

	err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1)
	if !errors.Is(err, workflow.ErrSubmitOrder) {
		t.Fatalf("Decide: got %v want ErrSubmitOrder", err)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalApproved {
		t.Fatalf("approval status: got %q want approved", approval.Status)
	}
	if approval.Note != "ok" || approval.DecidedBy == nil || *approval.DecidedBy != 1 {
		t.Fatalf("approval: %+v", approval)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalRejected {
		t.Fatalf("proposal status: got %q want rejected", proposal.Status)
	}
	if !strings.Contains(proposal.BreachReasonsJSON, "broker:") {
		t.Fatalf("proposal breach reasons: %s", proposal.BreachReasonsJSON)
	}
	if !strings.Contains(proposal.BreachReasonsJSON, submitErr.Error()) {
		t.Fatalf("proposal breach reasons must include submit error: %s", proposal.BreachReasonsJSON)
	}

	var orders int64
	if err := gormDB.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}

	var run models.WorkflowRun
	if err := gormDB.First(&run, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}
}

func TestDecideRejectedMarksProposalRejected(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t, nil)

	if err := svc.Decide(context.Background(), approvalID, approvals.DecisionRejected, "too big", 1); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalRejected {
		t.Fatalf("approval status: got %q", approval.Status)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalRejected {
		t.Fatalf("proposal status: got %q want rejected", proposal.Status)
	}

	var run models.WorkflowRun
	if err := gormDB.First(&run, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != workflow.StatusExecuted {
		t.Fatalf("run status: got %q want executed", run.Status)
	}

	var orders int64
	if err := gormDB.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 0 {
		t.Fatalf("orders: got %d want 0", orders)
	}

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash changed: got %v", account.Cash)
	}
}

func TestCancelRunOnAwaitingApprovalCancelsAndUpsertsNAV(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t, nil)

	if err := svc.CancelRun(context.Background(), runID); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}

	var run models.WorkflowRun
	if err := gormDB.First(&run, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != workflow.StatusCancelled {
		t.Fatalf("run status: got %q want cancelled", run.Status)
	}

	var approval models.Approval
	if err := gormDB.First(&approval, approvalID).Error; err != nil {
		t.Fatalf("approval: %v", err)
	}
	if approval.Status != workflow.ApprovalCancelled {
		t.Fatalf("approval status: got %q", approval.Status)
	}

	var proposal models.TradeProposal
	if err := gormDB.Where("run_id = ?", runID).First(&proposal).Error; err != nil {
		t.Fatalf("proposal: %v", err)
	}
	if proposal.Status != workflow.ProposalCancelled {
		t.Fatalf("proposal status: got %q", proposal.Status)
	}

	var nav models.NavSnapshot
	if err := gormDB.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Nav != 100000 {
		t.Fatalf("nav: got %v want 100000", nav.Nav)
	}
}

func TestDecideApprovedTwiceDoesNotDoubleFill(t *testing.T) {
	fb := &fakeBroker{acct: broker.Account{Cash: 100000, Equity: 100000}}
	svc, gormDB, _, approvalID := setupPendingApproval(t, fb)

	if err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1); err != nil {
		t.Fatalf("Decide first: %v", err)
	}
	err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "again", 1)
	if !errors.Is(err, approvals.ErrApprovalNotPending) {
		t.Fatalf("Decide second: got %v want ErrApprovalNotPending", err)
	}

	var orders int64
	if err := gormDB.Model(&models.Order{}).Count(&orders).Error; err != nil {
		t.Fatalf("orders: %v", err)
	}
	if orders != 1 {
		t.Fatalf("orders: got %d want 1 (no double fill)", orders)
	}

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	if account.Cash != 100000 {
		t.Fatalf("cash: got %v want %v (no local debit)", account.Cash, 100000)
	}
}

func TestCancelRunOnExecutedDoesNotFlipToCancelled(t *testing.T) {
	fb := &fakeBroker{acct: broker.Account{Cash: 100000, Equity: 100000}}
	svc, gormDB, runID, approvalID := setupPendingApproval(t, fb)

	if err := svc.Decide(context.Background(), approvalID, approvals.DecisionApproved, "ok", 1); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var runBefore models.WorkflowRun
	if err := gormDB.First(&runBefore, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if runBefore.Status != workflow.StatusExecuted {
		t.Fatalf("run status before cancel: got %q want executed", runBefore.Status)
	}

	err := svc.CancelRun(context.Background(), runID)
	if !errors.Is(err, approvals.ErrRunNotCancellable) {
		t.Fatalf("CancelRun: got %v want ErrRunNotCancellable", err)
	}

	var runAfter models.WorkflowRun
	if err := gormDB.First(&runAfter, runID).Error; err != nil {
		t.Fatalf("run: %v", err)
	}
	if runAfter.Status != workflow.StatusExecuted {
		t.Fatalf("run status after cancel: got %q want executed", runAfter.Status)
	}
}

func setupPendingApproval(t *testing.T, br broker.Client) (*approvals.Service, *gorm.DB, uint, uint) {
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

	run := models.WorkflowRun{TradeDate: tradeDate, Status: workflow.StatusAwaitingApproval}
	if err := gormDB.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	proposal := models.TradeProposal{
		RunID:               run.ID,
		Symbol:              "AAPL",
		Side:                "buy",
		Qty:                 100,
		EstimatedNotional:   19100,
		EstimatedCashImpact: -19100,
		Status:              workflow.ProposalAwaitingApproval,
	}
	if err := gormDB.Create(&proposal).Error; err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	approval := models.Approval{
		ProposalID:        proposal.ID,
		Status:            workflow.ApprovalPending,
		BreachReasonsJSON: `["max_order_notional"]`,
	}
	if err := gormDB.Create(&approval).Error; err != nil {
		t.Fatalf("create approval: %v", err)
	}

	svc := &approvals.Service{DB: gormDB, Ledger: &ledger.Service{DB: gormDB}, Broker: br}
	return svc, gormDB, run.ID, approval.ID
}
