package ledger

import (
	"context"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

type AccountSnapshot struct {
	Cash      float64            `json:"cash"`
	Currency  string             `json:"currency"`
	Positions []SnapshotPosition `json:"positions"`
}

type SnapshotPosition struct {
	Symbol     string   `json:"symbol"`
	Qty        float64  `json:"qty"`
	AvgCost    float64  `json:"avg_cost"`
	StopLoss   *float64 `json:"stop_loss"`
	TakeProfit *float64 `json:"take_profit"`
}

func (s *Service) AccountSnapshot(ctx context.Context, accountID uint) (AccountSnapshot, error) {
	var account models.Account
	if err := s.DB.WithContext(ctx).First(&account, accountID).Error; err != nil {
		return AccountSnapshot{}, err
	}

	var positions []models.Position
	if err := s.DB.WithContext(ctx).Where("account_id = ?", accountID).Find(&positions).Error; err != nil {
		return AccountSnapshot{}, err
	}

	snap := AccountSnapshot{
		Cash:      account.Cash,
		Currency:  account.Currency,
		Positions: make([]SnapshotPosition, 0, len(positions)),
	}
	for _, p := range positions {
		snap.Positions = append(snap.Positions, SnapshotPosition{
			Symbol:     p.Symbol,
			Qty:        p.Qty,
			AvgCost:    p.AvgCost,
			StopLoss:   p.StopLoss,
			TakeProfit: p.TakeProfit,
		})
	}
	return snap, nil
}
