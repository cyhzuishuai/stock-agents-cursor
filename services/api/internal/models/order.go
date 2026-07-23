package models

type Order struct {
	ID         uint    `gorm:"primaryKey" json:"id"`
	AccountID  uint    `json:"account_id"`
	RunID      *uint   `json:"run_id"`
	ApprovalID *uint   `json:"approval_id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Qty        float64 `json:"qty"`
	FillPrice  float64 `json:"fill_price"`
	Notional   float64 `json:"notional"`
	Status     string  `json:"status"` // filled
	TradeDate  string  `json:"trade_date"`
}
