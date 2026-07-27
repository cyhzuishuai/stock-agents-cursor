package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

type Service struct{ DB *gorm.DB }

func (s *Service) ApplyFill(ctx context.Context, req FillRequest) (models.Order, error) {
	if req.Side != "buy" {
		return models.Order{}, fmt.Errorf("unsupported side %q", req.Side)
	}

	notional := req.Qty * req.FillPrice
	var order models.Order

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account models.Account
		if err := tx.First(&account, req.AccountID).Error; err != nil {
			return err
		}
		if account.Cash < notional {
			return fmt.Errorf("insufficient cash: have %v need %v", account.Cash, notional)
		}
		account.Cash -= notional
		if err := tx.Save(&account).Error; err != nil {
			return err
		}

		var pos models.Position
		err := tx.Where("account_id = ? AND symbol = ?", req.AccountID, req.Symbol).First(&pos).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			pos = models.Position{
				AccountID: req.AccountID,
				Symbol:    req.Symbol,
				Qty:       req.Qty,
				AvgCost:   req.FillPrice,
			}
			if err := tx.Create(&pos).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			totalCost := pos.Qty*pos.AvgCost + notional
			pos.Qty += req.Qty
			pos.AvgCost = totalCost / pos.Qty
			if err := tx.Save(&pos).Error; err != nil {
				return err
			}
		}

		order = models.Order{
			AccountID:  req.AccountID,
			RunID:      req.RunID,
			ApprovalID: req.ApprovalID,
			Symbol:     req.Symbol,
			Side:       req.Side,
			Qty:        req.Qty,
			FillPrice:  req.FillPrice,
			Notional:   notional,
			Status:     "filled",
			TradeDate:  req.TradeDate,
		}
		return tx.Create(&order).Error
	})
	if err != nil {
		return models.Order{}, err
	}
	return order, nil
}

func (s *Service) UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error) {
	return models.NavSnapshot{}, errors.New("not implemented")
}
