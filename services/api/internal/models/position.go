package models

type Position struct {
	ID         uint     `gorm:"primaryKey" json:"id"`
	AccountID  uint     `gorm:"index" json:"account_id"`
	Symbol     string   `gorm:"index" json:"symbol"`
	Qty        float64  `json:"qty"`
	AvgCost    float64  `json:"avg_cost"`
	StopLoss   *float64 `json:"stop_loss"`
	TakeProfit *float64 `json:"take_profit"`
}
