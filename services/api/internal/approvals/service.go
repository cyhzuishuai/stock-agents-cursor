package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/broker"
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
	UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error)
}

type Service struct {
	DB     *gorm.DB
	Ledger LedgerAPI
	Broker broker.Client
}

func (s *Service) Decide(ctx context.Context, approvalID uint, decision, note string, userID uint) error {
	if decision != DecisionApproved && decision != DecisionRejected {
		return ErrInvalidDecision
	}
	if decision == DecisionApproved && s.Broker == nil {
		return workflow.ErrBrokerNotConfigured
	}

	var run models.WorkflowRun
	var proposal models.TradeProposal
	var accountID uint

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

		if decision == DecisionApproved {
			account, err := loadAccountTx(tx)
			if err != nil {
				return err
			}
			accountID = account.ID
			approvalUpdates["status"] = workflow.ApprovalApproved
			if err := tx.Model(&approval).Updates(approvalUpdates).Error; err != nil {
				return fmt.Errorf("update approval: %w", err)
			}
			// Proposal status is updated by workflow.SubmitProposal after broker sync.
			return nil
		}

		if err := tx.Model(&approval).Updates(approvalUpdates).Error; err != nil {
			return fmt.Errorf("update approval: %w", err)
		}
		if err := tx.Model(&proposal).Update("status", workflow.ProposalRejected).Error; err != nil {
			return fmt.Errorf("update proposal: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if decision == DecisionApproved {
		if err := workflow.SubmitProposal(ctx, s.DB, s.Broker, accountID, &run, &proposal); err != nil {
			if errors.Is(err, workflow.ErrSubmitOrder) {
				reasons, mErr := json.Marshal([]string{fmt.Sprintf("broker: %v", err)})
				if mErr != nil {
					return mErr
				}
				if uErr := s.DB.WithContext(ctx).Model(&proposal).Updates(map[string]any{
					"status":              workflow.ProposalRejected,
					"breach_reasons_json": string(reasons),
				}).Error; uErr != nil {
					return fmt.Errorf("reject proposal after submit failure: %w", uErr)
				}
			}
			_ = s.refreshRunStatus(ctx, run.ID)
			return err
		}
	}

	if err := s.refreshRunStatus(ctx, run.ID); err != nil {
		return err
	}
	if err := s.upsertNAV(ctx, run); err != nil {
		return err
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
	if err := s.upsertNAVWithMarks(ctx, run.TradeDate, marks); err != nil {
		return err
	}
	return nil
}

func (s *Service) upsertNAV(ctx context.Context, run models.WorkflowRun) error {
	marks, err := s.loadMarks(ctx, run.ID)
	if err != nil {
		return err
	}
	return s.upsertNAVWithMarks(ctx, run.TradeDate, marks)
}

func (s *Service) upsertNAVWithMarks(ctx context.Context, tradeDate string, marks map[string]float64) error {
	if s.Broker != nil {
		acct, err := s.Broker.GetAccount(ctx)
		if err == nil {
			cash := acct.Cash
			nav := acct.Equity
			if nav == 0 && acct.PortfolioValue > 0 {
				nav = acct.PortfolioValue
			}
			equity := nav - cash
			if equity < 0 {
				equity = 0
			}
			snap := models.NavSnapshot{
				TradeDate: tradeDate,
				Cash:      cash,
				Equity:    equity,
				Nav:       nav,
			}
			var existing models.NavSnapshot
			if err := s.DB.WithContext(ctx).Where("trade_date = ?", tradeDate).Limit(1).Find(&existing).Error; err != nil {
				return err
			}
			if existing.ID == 0 {
				return s.DB.WithContext(ctx).Create(&snap).Error
			}
			snap.ID = existing.ID
			return s.DB.WithContext(ctx).Save(&snap).Error
		}
	}
	if _, err := s.Ledger.UpsertNAV(ctx, tradeDate, marks); err != nil {
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
	marks := map[string]float64{}
	if s.Broker != nil {
		positions, err := s.Broker.ListPositions(ctx)
		if err == nil {
			for _, p := range positions {
				if p.CurrentPrice > 0 {
					marks[p.Symbol] = p.CurrentPrice
				}
			}
		}
	}

	var proposals []models.TradeProposal
	if err := s.DB.WithContext(ctx).Where("run_id = ?", runID).Find(&proposals).Error; err != nil {
		return nil, fmt.Errorf("load proposals for marks: %w", err)
	}
	for _, p := range proposals {
		if _, ok := marks[p.Symbol]; ok {
			continue
		}
		if p.Qty > 0 && p.EstimatedNotional > 0 {
			marks[p.Symbol] = p.EstimatedNotional / p.Qty
		}
	}
	if len(marks) == 0 {
		return nil, ErrMissingFillPrice
	}
	return marks, nil
}

func loadAccountTx(tx *gorm.DB) (models.Account, error) {
	var account models.Account
	if err := tx.First(&account).Error; err != nil {
		return models.Account{}, fmt.Errorf("load account: %w", err)
	}
	return account, nil
}
