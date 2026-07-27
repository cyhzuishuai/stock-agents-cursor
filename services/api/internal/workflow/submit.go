package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

// SubmitProposal submits a market order to the broker, mirrors it locally, and polls until
// terminal status (filled/canceled/rejected) or timeout. Extracted for reuse by approvals (Task 5).
func SubmitProposal(ctx context.Context, db *gorm.DB, br broker.Client, accountID uint, run *models.WorkflowRun, p *models.TradeProposal) error {
	if br == nil {
		return fmt.Errorf("broker not configured")
	}
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if run == nil || p == nil {
		return fmt.Errorf("run and proposal are required")
	}

	clientOrderID := fmt.Sprintf("prop-%d", p.ID)
	bo, err := br.SubmitOrder(ctx, broker.OrderRequest{
		Symbol:        p.Symbol,
		Side:          p.Side,
		Qty:           p.Qty,
		ClientOrderID: clientOrderID,
		Type:          "market",
		TimeInForce:   "day",
	})
	if err != nil {
		return err
	}

	runID := run.ID
	propID := p.ID
	order := models.Order{
		AccountID:     accountID,
		RunID:         &runID,
		ProposalID:    &propID,
		Symbol:        p.Symbol,
		Side:          p.Side,
		Qty:           p.Qty,
		Status:        "submitted",
		TradeDate:     run.TradeDate,
		BrokerOrderID: bo.ID,
		ClientOrderID: clientOrderID,
	}
	if err := db.WithContext(ctx).Create(&order).Error; err != nil {
		return fmt.Errorf("create order mirror: %w", err)
	}
	p.Status = ProposalSubmitted
	if err := db.WithContext(ctx).Model(p).Update("status", ProposalSubmitted).Error; err != nil {
		return fmt.Errorf("mark proposal submitted: %w", err)
	}

	deadline := time.Now().Add(BrokerSyncTimeout)
	for {
		cur, err := br.GetOrder(ctx, bo.ID)
		if err != nil {
			return fmt.Errorf("get broker order: %w", err)
		}
		if isBrokerTerminal(cur.Status) {
			return applyBrokerTerminal(ctx, db, p, &order, cur)
		}
		if time.Now().After(deadline) {
			// Leave proposal/order as submitted for later reconciliation.
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(BrokerSyncPollInterval):
		}
	}
}

func (r *Runner) submitAndSync(ctx context.Context, accountID uint, run *models.WorkflowRun, p *models.TradeProposal) error {
	return SubmitProposal(ctx, r.DB, r.Broker, accountID, run, p)
}

func isBrokerTerminal(status string) bool {
	switch status {
	case "filled", "canceled", "cancelled", "rejected", "expired":
		return true
	default:
		return false
	}
}

func applyBrokerTerminal(ctx context.Context, db *gorm.DB, p *models.TradeProposal, order *models.Order, cur broker.Order) error {
	switch cur.Status {
	case "filled":
		fillPrice := cur.FilledAvgPrice
		notional := cur.FilledQty * fillPrice
		if cur.FilledQty <= 0 {
			notional = order.Qty * fillPrice
		}
		order.Status = "filled"
		order.FillPrice = fillPrice
		order.Notional = notional
		if cur.FilledQty > 0 {
			order.Qty = cur.FilledQty
		}
		if err := db.WithContext(ctx).Model(order).Updates(map[string]any{
			"status":     order.Status,
			"fill_price": order.FillPrice,
			"notional":   order.Notional,
			"qty":        order.Qty,
		}).Error; err != nil {
			return fmt.Errorf("update order mirror filled: %w", err)
		}
		p.Status = ProposalFilled
		return db.WithContext(ctx).Model(p).Update("status", ProposalFilled).Error
	default:
		status := cur.Status
		if status == "cancelled" {
			status = "canceled"
		}
		order.Status = status
		if err := db.WithContext(ctx).Model(order).Update("status", status).Error; err != nil {
			return fmt.Errorf("update order mirror %s: %w", status, err)
		}
		p.Status = ProposalRejected
		return db.WithContext(ctx).Model(p).Update("status", ProposalRejected).Error
	}
}
