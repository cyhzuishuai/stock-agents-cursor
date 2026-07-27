package approvals_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"gorm.io/gorm"
)

const tradeDate = "2026-07-22"

func TestDecideApprovedFillsAndExecutesRun(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t)

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
	if len(orders) != 1 || orders[0].FillPrice != 191 || orders[0].Qty != 100 {
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
	if account.Cash != 100000-19100 {
		t.Fatalf("cash: got %v want %v", account.Cash, 100000-19100)
	}

	var nav models.NavSnapshot
	if err := gormDB.Where("trade_date = ?", tradeDate).First(&nav).Error; err != nil {
		t.Fatalf("nav: %v", err)
	}
	if nav.Nav != 100000 {
		t.Fatalf("nav: got %v want 100000", nav.Nav)
	}
}

func TestDecideRejectedMarksProposalRejected(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t)

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

func TestCancelRunCancelsPending(t *testing.T) {
	svc, gormDB, runID, approvalID := setupPendingApproval(t)

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
}

func setupPendingApproval(t *testing.T) (*approvals.Service, *gorm.DB, uint, uint) {
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
	step := models.WorkflowStepResult{
		RunID:       run.ID,
		Step:        workflow.StepData,
		Status:      workflow.StepStatusOK,
		PayloadJSON: fmt.Sprintf(`{"bars":[{"symbol":"AAPL","trade_date":"%s","close":191}],"warnings":[]}`, tradeDate),
	}
	if err := gormDB.Create(&step).Error; err != nil {
		t.Fatalf("create step: %v", err)
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

	svc := &approvals.Service{DB: gormDB, Ledger: &ledger.Service{DB: gormDB}}
	return svc, gormDB, run.ID, approval.ID
}
