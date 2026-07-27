package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
)

var (
	ErrApprovalNotFound   = errors.New("approval not found")
	ErrApprovalNotPending = errors.New("approval not pending")
	ErrInvalidDecision    = errors.New("invalid decision")
	ErrRunNotFound        = errors.New("run not found")
	ErrRunNotCancellable  = errors.New("run not cancellable")
	ErrMissingFillPrice   = errors.New("missing fill price")
)

// LedgerAPI is the ledger surface used by approvals.
type LedgerAPI interface {
	ApplyFill(ctx context.Context, req ledger.FillRequest) (models.Order, error)
	UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error)
}

type Service struct {
	DB     *gorm.DB
	Ledger LedgerAPI
}

type dataBar struct {
	Symbol string  `json:"symbol"`
	Close  float64 `json:"close"`
}

type dataResult struct {
	Bars []dataBar `json:"bars"`
}

func (s *Service) Decide(ctx context.Context, approvalID uint, decision, note string, userID uint) error {
	if decision != DecisionApproved && decision != DecisionRejected {
		return ErrInvalidDecision
	}

	var run models.WorkflowRun
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var approval models.Approval
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&approval, approvalID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrApprovalNotFound
			}
			return err
		}
		if approval.Status != workflow.ApprovalPending {
			return ErrApprovalNotPending
		}

		var proposal models.TradeProposal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&proposal, approval.ProposalID).Error; err != nil {
			return fmt.Errorf("load proposal: %w", err)
		}
		if proposal.Status != workflow.ProposalAwaitingApproval {
			return ErrApprovalNotPending
		}

		if err := tx.First(&run, proposal.RunID).Error; err != nil {
			return fmt.Errorf("load run: %w", err)
		}

		decidedBy := userID
		approvalUpdates := map[string]any{
			"status":     workflow.ApprovalRejected,
			"note":       note,
			"decided_by": decidedBy,
		}
		proposalStatus := workflow.ProposalRejected

		if decision == DecisionApproved {
			marks, err := loadMarksTx(tx, run.ID)
			if err != nil {
				return err
			}
			account, err := loadAccountTx(tx)
			if err != nil {
				return err
			}
			fillPrice, err := fillPriceForProposal(proposal, marks)
			if err != nil {
				return err
			}
			runID := run.ID
			approvalIDCopy := approval.ID
			if _, err := ledger.ApplyFillTx(tx, ledger.FillRequest{
				AccountID:  account.ID,
				RunID:      &runID,
				ApprovalID: &approvalIDCopy,
				Symbol:     proposal.Symbol,
				Side:       proposal.Side,
				Qty:        proposal.Qty,
				FillPrice:  fillPrice,
				TradeDate:  run.TradeDate,
				StopLoss:   proposal.StopLoss,
				TakeProfit: proposal.TakeProfit,
			}); err != nil {
				return fmt.Errorf("apply fill: %w", err)
			}
			approvalUpdates["status"] = workflow.ApprovalApproved
			proposalStatus = workflow.ProposalFilled
		}

		if err := tx.Model(&approval).Updates(approvalUpdates).Error; err != nil {
			return fmt.Errorf("update approval: %w", err)
		}
		if err := tx.Model(&proposal).Update("status", proposalStatus).Error; err != nil {
			return fmt.Errorf("update proposal: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := s.refreshRunStatus(ctx, run.ID); err != nil {
		return err
	}
	marks, err := s.loadMarks(ctx, run.ID)
	if err != nil {
		return err
	}
	if _, err := s.Ledger.UpsertNAV(ctx, run.TradeDate, marks); err != nil {
		return fmt.Errorf("upsert nav: %w", err)
	}
	return nil
}

func (s *Service) CancelRun(ctx context.Context, runID uint) error {
	var run models.WorkflowRun
	if err := s.DB.WithContext(ctx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRunNotFound
		}
		return err
	}
	if run.Status != workflow.StatusAwaitingApproval {
		return ErrRunNotCancellable
	}

	marks, err := s.loadMarks(ctx, runID)
	if err != nil {
		return err
	}

	var proposalIDs []uint
	if err := s.DB.WithContext(ctx).Model(&models.TradeProposal{}).
		Where("run_id = ? AND status = ?", runID, workflow.ProposalAwaitingApproval).
		Pluck("id", &proposalIDs).Error; err != nil {
		return err
	}

	if len(proposalIDs) > 0 {
		if err := s.DB.WithContext(ctx).Model(&models.TradeProposal{}).
			Where("id IN ?", proposalIDs).
			Update("status", workflow.ProposalCancelled).Error; err != nil {
			return fmt.Errorf("cancel proposals: %w", err)
		}
		if err := s.DB.WithContext(ctx).Model(&models.Approval{}).
			Where("proposal_id IN ? AND status = ?", proposalIDs, workflow.ApprovalPending).
			Update("status", workflow.ApprovalCancelled).Error; err != nil {
			return fmt.Errorf("cancel approvals: %w", err)
		}
	}

	if err := s.DB.WithContext(ctx).Model(&run).Update("status", workflow.StatusCancelled).Error; err != nil {
		return err
	}
	if _, err := s.Ledger.UpsertNAV(ctx, run.TradeDate, marks); err != nil {
		return fmt.Errorf("upsert nav: %w", err)
	}
	return nil
}

func (s *Service) refreshRunStatus(ctx context.Context, runID uint) error {
	var pendingApprovals int64
	if err := s.DB.WithContext(ctx).Model(&models.Approval{}).
		Joins("JOIN trade_proposals ON trade_proposals.id = approvals.proposal_id").
		Where("trade_proposals.run_id = ? AND approvals.status = ?", runID, workflow.ApprovalPending).
		Count(&pendingApprovals).Error; err != nil {
		return fmt.Errorf("count pending approvals: %w", err)
	}

	var awaitingProposals int64
	if err := s.DB.WithContext(ctx).Model(&models.TradeProposal{}).
		Where("run_id = ? AND status = ?", runID, workflow.ProposalAwaitingApproval).
		Count(&awaitingProposals).Error; err != nil {
		return fmt.Errorf("count awaiting proposals: %w", err)
	}

	status := workflow.StatusExecuted
	if pendingApprovals > 0 || awaitingProposals > 0 {
		status = workflow.StatusAwaitingApproval
	}
	return s.DB.WithContext(ctx).Model(&models.WorkflowRun{}).Where("id = ?", runID).Update("status", status).Error
}

func (s *Service) loadMarks(ctx context.Context, runID uint) (map[string]float64, error) {
	return loadMarksTx(s.DB.WithContext(ctx), runID)
}

func loadMarksTx(tx *gorm.DB, runID uint) (map[string]float64, error) {
	var step models.WorkflowStepResult
	if err := tx.Where("run_id = ? AND step = ?", runID, workflow.StepData).First(&step).Error; err != nil {
		return nil, fmt.Errorf("load data step: %w", err)
	}
	var data dataResult
	if err := json.Unmarshal([]byte(step.PayloadJSON), &data); err != nil {
		return nil, fmt.Errorf("parse data step: %w", err)
	}
	marks := make(map[string]float64, len(data.Bars))
	for _, b := range data.Bars {
		if b.Symbol == "" {
			continue
		}
		marks[b.Symbol] = b.Close
	}
	if len(marks) == 0 {
		return nil, ErrMissingFillPrice
	}
	return marks, nil
}

func fillPriceForProposal(proposal models.TradeProposal, marks map[string]float64) (float64, error) {
	if price, ok := marks[proposal.Symbol]; ok && price > 0 {
		return price, nil
	}
	if proposal.Qty > 0 && proposal.EstimatedNotional > 0 {
		return proposal.EstimatedNotional / proposal.Qty, nil
	}
	return 0, fmt.Errorf("%w: %s", ErrMissingFillPrice, proposal.Symbol)
}

func loadAccountTx(tx *gorm.DB) (models.Account, error) {
	var account models.Account
	if err := tx.First(&account).Error; err != nil {
		return models.Account{}, fmt.Errorf("load account: %w", err)
	}
	return account, nil
}
