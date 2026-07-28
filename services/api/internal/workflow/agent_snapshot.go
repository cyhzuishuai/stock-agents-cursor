package workflow

import (
	"context"
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
)

// AgentAccountSnapshot is the Alpaca-authoritative account view injected into
// agent-runtime requests. It is intentionally separate from ledger.AccountSnapshot.
type AgentAccountSnapshot struct {
	Cash       float64             `json:"cash"`
	Equity     float64             `json:"equity"`
	Currency   string              `json:"currency"`
	Positions  []AgentPosition     `json:"positions"`
	OpenOrders []AgentOpenOrder    `json:"open_orders"`
}

// AgentPosition is a position entry in AgentAccountSnapshot.
type AgentPosition struct {
	Symbol     string   `json:"symbol"`
	Qty        float64  `json:"qty"`
	AvgCost    float64  `json:"avg_cost"`
	StopLoss   *float64 `json:"stop_loss,omitempty"`
	TakeProfit *float64 `json:"take_profit,omitempty"`
}

// AgentOpenOrder is an open order entry in AgentAccountSnapshot.
type AgentOpenOrder struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Qty           float64 `json:"qty"`
	Status        string  `json:"status"`
	ClientOrderID string  `json:"client_order_id,omitempty"`
}

// AgentRiskContext is the read-only risk view injected into agent-runtime requests.
type AgentRiskContext struct {
	ExecutionMode string             `json:"execution_mode"`
	Rules         AgentRiskRules     `json:"rules"`
}

// AgentRiskRules mirrors Go risk_rule_configs thresholds for agent tools.
type AgentRiskRules struct {
	MaxOrderNotional    float64 `json:"max_order_notional"`
	MaxSingleNameWeight float64 `json:"max_single_name_weight"`
	MinCashRatio        float64 `json:"min_cash_ratio"`
}

func buildAgentSnapshot(ctx context.Context, b broker.Client) (AgentAccountSnapshot, error) {
	if b == nil {
		return AgentAccountSnapshot{}, fmt.Errorf("broker is required")
	}

	acct, err := b.GetAccount(ctx)
	if err != nil {
		return AgentAccountSnapshot{}, fmt.Errorf("get account: %w", err)
	}

	positions, err := b.ListPositions(ctx)
	if err != nil {
		return AgentAccountSnapshot{}, fmt.Errorf("list positions: %w", err)
	}

	orders, err := b.ListOrders(ctx, "open")
	if err != nil {
		return AgentAccountSnapshot{}, fmt.Errorf("list open orders: %w", err)
	}

	snap := AgentAccountSnapshot{
		Cash:       acct.Cash,
		Equity:     acct.Equity,
		Currency:   "USD",
		Positions:  make([]AgentPosition, 0, len(positions)),
		OpenOrders: make([]AgentOpenOrder, 0, len(orders)),
	}
	for _, p := range positions {
		snap.Positions = append(snap.Positions, AgentPosition{
			Symbol:  p.Symbol,
			Qty:     p.Qty,
			AvgCost: p.AvgCost,
		})
	}
	for _, o := range orders {
		snap.OpenOrders = append(snap.OpenOrders, AgentOpenOrder{
			ID:            o.ID,
			Symbol:        o.Symbol,
			Side:          o.Side,
			Qty:           o.Qty,
			Status:        o.Status,
			ClientOrderID: o.ClientOrderID,
		})
	}
	return snap, nil
}

func buildRiskContext(executionMode string, cfg *config.Config) AgentRiskContext {
	rules := AgentRiskRules{}
	if cfg != nil {
		rules.MaxOrderNotional = cfg.RiskMaxOrderNotional
		rules.MaxSingleNameWeight = cfg.RiskMaxSingleNameWeight
		rules.MinCashRatio = cfg.RiskMinCashRatio
	}
	return AgentRiskContext{
		ExecutionMode: executionMode,
		Rules:         rules,
	}
}
