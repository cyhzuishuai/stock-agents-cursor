package models

type TradeProposal struct {
	ID                  uint     `gorm:"primaryKey" json:"id"`
	RunID               uint     `gorm:"index" json:"run_id"`
	Symbol              string   `json:"symbol"`
	Side                string   `json:"side"`
	Qty                 float64  `json:"qty"`
	TargetWeight        *float64 `json:"target_weight"`
	StopLoss            *float64 `json:"stop_loss"`
	TakeProfit          *float64 `json:"take_profit"`
	EstimatedNotional   float64  `json:"estimated_notional"`
	EstimatedCashImpact float64  `json:"estimated_cash_impact"`
	Status              string   `json:"status"` // pending_auto|awaiting_approval|filled|rejected|cancelled
	BreachReasonsJSON   string   `gorm:"type:text" json:"breach_reasons_json"`
}
