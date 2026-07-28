package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
)

type snapshotFakeBroker struct {
	acct       broker.Account
	pos        []broker.Position
	orders     []broker.Order
	acctErr    error
	posErr     error
	ordErr     error
	listStatus string
}

func (f *snapshotFakeBroker) GetAccount(ctx context.Context) (broker.Account, error) {
	if f.acctErr != nil {
		return broker.Account{}, f.acctErr
	}
	return f.acct, nil
}

func (f *snapshotFakeBroker) ListPositions(ctx context.Context) ([]broker.Position, error) {
	if f.posErr != nil {
		return nil, f.posErr
	}
	return f.pos, nil
}

func (f *snapshotFakeBroker) SubmitOrder(ctx context.Context, req broker.OrderRequest) (broker.Order, error) {
	return broker.Order{}, errors.New("not implemented")
}

func (f *snapshotFakeBroker) GetOrder(ctx context.Context, brokerOrderID string) (broker.Order, error) {
	return broker.Order{}, errors.New("not implemented")
}

func (f *snapshotFakeBroker) ListOrders(ctx context.Context, status string) ([]broker.Order, error) {
	f.listStatus = status
	if f.ordErr != nil {
		return nil, f.ordErr
	}
	return f.orders, nil
}

func TestBuildAgentSnapshotShape(t *testing.T) {
	fb := &snapshotFakeBroker{
		acct: broker.Account{Cash: 100000, Equity: 101800},
		pos: []broker.Position{
			{Symbol: "AAPL", Qty: 10, AvgCost: 180},
		},
		orders: []broker.Order{
			{
				ID:            "ord-001",
				ClientOrderID: "cli-msft-001",
				Symbol:        "MSFT",
				Side:          "buy",
				Qty:           5,
				Status:        "open",
			},
		},
	}

	snap, err := buildAgentSnapshot(context.Background(), fb)
	if err != nil {
		t.Fatalf("buildAgentSnapshot: %v", err)
	}
	if fb.listStatus != "open" {
		t.Fatalf("ListOrders status: got %q want %q", fb.listStatus, "open")
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"cash", "equity", "currency", "positions", "open_orders"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing json key %q in %s", key, string(raw))
		}
	}
	if m["cash"] != 100000.0 {
		t.Fatalf("cash: got %v", m["cash"])
	}
	if m["equity"] != 101800.0 {
		t.Fatalf("equity: got %v", m["equity"])
	}
	if m["currency"] != "USD" {
		t.Fatalf("currency: got %v", m["currency"])
	}

	positions, ok := m["positions"].([]any)
	if !ok || len(positions) != 1 {
		t.Fatalf("positions: got %#v", m["positions"])
	}
	p0 := positions[0].(map[string]any)
	if p0["symbol"] != "AAPL" || p0["qty"] != 10.0 || p0["avg_cost"] != 180.0 {
		t.Fatalf("position: %#v", p0)
	}

	orders, ok := m["open_orders"].([]any)
	if !ok || len(orders) != 1 {
		t.Fatalf("open_orders: got %#v", m["open_orders"])
	}
	o0 := orders[0].(map[string]any)
	if o0["id"] != "ord-001" || o0["symbol"] != "MSFT" || o0["side"] != "buy" ||
		o0["qty"] != 5.0 || o0["status"] != "open" || o0["client_order_id"] != "cli-msft-001" {
		t.Fatalf("open_order: %#v", o0)
	}
}

func TestBuildAgentSnapshotNilBroker(t *testing.T) {
	_, err := buildAgentSnapshot(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil broker")
	}
}

func TestBuildAgentSnapshotPropagatesErrors(t *testing.T) {
	t.Run("account", func(t *testing.T) {
		fb := &snapshotFakeBroker{acctErr: errors.New("acct down")}
		_, err := buildAgentSnapshot(context.Background(), fb)
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("positions", func(t *testing.T) {
		fb := &snapshotFakeBroker{
			acct:   broker.Account{Cash: 1, Equity: 1},
			posErr: errors.New("pos down"),
		}
		_, err := buildAgentSnapshot(context.Background(), fb)
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("orders", func(t *testing.T) {
		fb := &snapshotFakeBroker{
			acct:   broker.Account{Cash: 1, Equity: 1},
			ordErr: errors.New("orders down"),
		}
		_, err := buildAgentSnapshot(context.Background(), fb)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBuildRiskContext(t *testing.T) {
	cfg := &config.Config{
		RiskMaxOrderNotional:    10000,
		RiskMaxSingleNameWeight: 0.25,
		RiskMinCashRatio:        0.1,
	}
	rc := buildRiskContext("auto", cfg)
	raw, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["execution_mode"] != "auto" {
		t.Fatalf("execution_mode: got %v", m["execution_mode"])
	}
	rules, ok := m["rules"].(map[string]any)
	if !ok {
		t.Fatalf("rules: %#v", m["rules"])
	}
	if rules["max_order_notional"] != 10000.0 {
		t.Fatalf("max_order_notional: %v", rules["max_order_notional"])
	}
	if rules["max_single_name_weight"] != 0.25 {
		t.Fatalf("max_single_name_weight: %v", rules["max_single_name_weight"])
	}
	if rules["min_cash_ratio"] != 0.1 {
		t.Fatalf("min_cash_ratio: %v", rules["min_cash_ratio"])
	}
}
