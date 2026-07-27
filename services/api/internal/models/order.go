package models

type Order struct {
	ID            uint    `gorm:"primaryKey" json:"id"`
	AccountID     uint    `json:"account_id"`
	RunID         *uint   `json:"run_id"`
	ApprovalID    *uint   `json:"approval_id"`
	ProposalID    *uint   `json:"proposal_id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Qty           float64 `json:"qty"`
	FillPrice     float64 `json:"fill_price"`
	Notional      float64 `json:"notional"`
	Status        string  `json:"status"` // submitted|filled|rejected|canceled
	TradeDate     string  `json:"trade_date"`
	BrokerOrderID string  `gorm:"size:64;index" json:"broker_order_id"`
	ClientOrderID string  `gorm:"size:64;index" json:"client_order_id"`
}
