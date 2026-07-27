package broker

import "context"

type Account struct {
	ID             string
	Cash           float64
	Equity         float64
	BuyingPower    float64
	PortfolioValue float64
}

type Position struct {
	Symbol       string
	Qty          float64
	AvgCost      float64
	MarketValue  float64
	CurrentPrice float64
	UnrealizedPL float64
}

type OrderRequest struct {
	Symbol        string
	Side          string // buy|sell
	Qty           float64
	ClientOrderID string
	TimeInForce   string // day
	Type          string // market
}

type Order struct {
	ID             string
	ClientOrderID  string
	Symbol         string
	Side           string
	Qty            float64
	FilledQty      float64
	FilledAvgPrice float64
	Status         string // new|accepted|partially_filled|filled|canceled|rejected|...
}

type Client interface {
	GetAccount(ctx context.Context) (Account, error)
	ListPositions(ctx context.Context) ([]Position, error)
	SubmitOrder(ctx context.Context, req OrderRequest) (Order, error)
	GetOrder(ctx context.Context, brokerOrderID string) (Order, error)
	ListOrders(ctx context.Context, status string) ([]Order, error) // status e.g. "open"|"closed"|"all"
}
