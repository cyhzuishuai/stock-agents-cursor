package ledger

import (
	"context"
	"errors"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

var (
	ErrInsufficientCash = errors.New("insufficient cash")
	ErrInsufficientQty  = errors.New("insufficient qty")
	ErrInvalidSide      = errors.New("invalid side")
)

type Service struct{ DB *gorm.DB }

func (s *Service) ApplyFill(ctx context.Context, req FillRequest) (models.Order, error) {
	notional := req.Qty * req.FillPrice
	var order models.Order

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		switch req.Side {
		case "buy":
			if err := s.applyBuy(tx, req, notional); err != nil {
				return err
			}
		case "sell":
			if err := s.applySell(tx, req, notional); err != nil {
				return err
			}
		default:
			return ErrInvalidSide
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

func (s *Service) applyBuy(tx *gorm.DB, req FillRequest, notional float64) error {
	var account models.Account
	if err := tx.First(&account, req.AccountID).Error; err != nil {
		return err
	}
	if account.Cash < notional {
		return ErrInsufficientCash
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
		return tx.Create(&pos).Error
	case err != nil:
		return err
	default:
		totalCost := pos.Qty*pos.AvgCost + notional
		pos.Qty += req.Qty
		pos.AvgCost = totalCost / pos.Qty
		return tx.Save(&pos).Error
	}
}

func (s *Service) applySell(tx *gorm.DB, req FillRequest, notional float64) error {
	var pos models.Position
	err := tx.Where("account_id = ? AND symbol = ?", req.AccountID, req.Symbol).First(&pos).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrInsufficientQty
	}
	if err != nil {
		return err
	}
	if pos.Qty < req.Qty {
		return ErrInsufficientQty
	}

	var account models.Account
	if err := tx.First(&account, req.AccountID).Error; err != nil {
		return err
	}
	account.Cash += notional
	if err := tx.Save(&account).Error; err != nil {
		return err
	}

	pos.Qty -= req.Qty
	if pos.Qty == 0 {
		return tx.Delete(&pos).Error
	}
	return tx.Save(&pos).Error
}

func (s *Service) UpsertNAV(ctx context.Context, tradeDate string, marks map[string]float64) (models.NavSnapshot, error) {
	return models.NavSnapshot{}, errors.New("not implemented")
}
