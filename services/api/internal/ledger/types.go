package ledger

type FillRequest struct {
	AccountID  uint
	RunID      *uint
	ApprovalID *uint
	Symbol     string
	Side       string // buy|sell
	Qty        float64
	FillPrice  float64
	TradeDate  string
	StopLoss   *float64
	TakeProfit *float64
}
