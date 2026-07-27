package broker_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
)

func TestNewAlpacaRequiresCredentials(t *testing.T) {
	_, err := broker.NewAlpaca(&config.Config{
		AlpacaBaseURL: "https://paper-api.alpaca.markets",
	})
	if err == nil || !strings.Contains(err.Error(), "alpaca credentials required") {
		t.Fatalf("expected credentials error, got %v", err)
	}
}

func TestSubmitOrderMarket(t *testing.T) {
	var gotKey, gotSecret, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("APCA-API-KEY-ID")
		gotSecret = r.Header.Get("APCA-API-SECRET-KEY")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"oid","client_order_id":"42","symbol":"AAPL","side":"buy","qty":"1","filled_qty":"0","filled_avg_price":"0","status":"accepted"}`))
	}))
	defer srv.Close()

	client := &broker.Alpaca{
		BaseURL: srv.URL,
		Key:     "key-id",
		Secret:  "secret-key",
		HTTP:    srv.Client(),
	}

	order, err := client.SubmitOrder(context.Background(), broker.OrderRequest{
		Symbol:        "AAPL",
		Side:          "BUY",
		Qty:           1,
		ClientOrderID: "42",
		TimeInForce:   "day",
		Type:          "market",
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	if gotPath != "/v2/orders" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotKey != "key-id" || gotSecret != "secret-key" {
		t.Fatalf("headers: key=%q secret=%q", gotKey, gotSecret)
	}
	if gotBody["type"] != "market" || gotBody["time_in_force"] != "day" {
		t.Fatalf("body type/tif: %#v", gotBody)
	}
	if gotBody["side"] != "buy" {
		t.Fatalf("side should be lowercase buy, got %#v", gotBody["side"])
	}
	if gotBody["symbol"] != "AAPL" || gotBody["client_order_id"] != "42" {
		t.Fatalf("body symbol/client_order_id: %#v", gotBody)
	}
	if order.ID != "oid" || order.ClientOrderID != "42" || order.Status != "accepted" {
		t.Fatalf("order: %+v", order)
	}
	if order.Qty != 1 || order.FilledQty != 0 {
		t.Fatalf("order qty fields: %+v", order)
	}
}

func TestGetAccountAndPositions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/account":
			_, _ = w.Write([]byte(`{"id":"acct-1","cash":"1000.50","equity":"1500.25","buying_power":"2000","portfolio_value":"1500.25"}`))
		case "/v2/positions":
			_, _ = w.Write([]byte(`[{"symbol":"AAPL","qty":"2","avg_entry_price":"150.5","market_value":"320","current_price":"160","unrealized_pl":"19"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &broker.Alpaca{
		BaseURL: srv.URL,
		Key:     "k",
		Secret:  "s",
		HTTP:    srv.Client(),
	}

	acct, err := client.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if acct.ID != "acct-1" || acct.Cash != 1000.50 || acct.Equity != 1500.25 {
		t.Fatalf("account: %+v", acct)
	}
	if acct.BuyingPower != 2000 || acct.PortfolioValue != 1500.25 {
		t.Fatalf("account power/value: %+v", acct)
	}

	positions, err := client.ListPositions(context.Background())
	if err != nil {
		t.Fatalf("ListPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions len: %d", len(positions))
	}
	p := positions[0]
	if p.Symbol != "AAPL" || p.Qty != 2 || p.AvgCost != 150.5 || p.MarketValue != 320 || p.CurrentPrice != 160 || p.UnrealizedPL != 19 {
		t.Fatalf("position: %+v", p)
	}
}

func TestGetOrderAndListOrders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/orders/oid-1":
			_, _ = w.Write([]byte(`{"id":"oid-1","client_order_id":"c1","symbol":"MSFT","side":"sell","qty":"3","filled_qty":"3","filled_avg_price":"400.5","status":"filled"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/orders":
			if r.URL.Query().Get("status") != "open" {
				t.Errorf("expected status=open, got %q", r.URL.Query().Get("status"))
			}
			_, _ = w.Write([]byte(`[{"id":"oid-2","client_order_id":"c2","symbol":"GOOG","side":"buy","qty":1.5,"filled_qty":0,"filled_avg_price":0,"status":"new"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &broker.Alpaca{
		BaseURL: srv.URL,
		Key:     "k",
		Secret:  "s",
		HTTP:    srv.Client(),
	}

	order, err := client.GetOrder(context.Background(), "oid-1")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if order.ID != "oid-1" || order.Side != "sell" || order.FilledAvgPrice != 400.5 || order.Status != "filled" {
		t.Fatalf("order: %+v", order)
	}

	orders, err := client.ListOrders(context.Background(), "open")
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(orders) != 1 || orders[0].ID != "oid-2" || orders[0].Qty != 1.5 {
		t.Fatalf("orders: %+v", orders)
	}
}

func TestCachedClientTTL(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/account" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a","cash":"1","equity":"1","buying_power":"1","portfolio_value":"1"}`))
	}))
	defer srv.Close()

	inner := &broker.Alpaca{
		BaseURL: srv.URL,
		Key:     "k",
		Secret:  "s",
		HTTP:    srv.Client(),
	}
	cached := broker.NewCachedClient(inner, 50*time.Millisecond)

	if _, err := cached.GetAccount(context.Background()); err != nil {
		t.Fatalf("GetAccount #1: %v", err)
	}
	if _, err := cached.GetAccount(context.Background()); err != nil {
		t.Fatalf("GetAccount #2: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("within TTL expected 1 upstream call, got %d", calls.Load())
	}

	time.Sleep(60 * time.Millisecond)
	if _, err := cached.GetAccount(context.Background()); err != nil {
		t.Fatalf("GetAccount #3: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("after TTL expected 2 upstream calls, got %d", calls.Load())
	}
}

func TestCachedClientInvalidateOnSubmitOrder(t *testing.T) {
	var accountCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/account":
			accountCalls.Add(1)
			_, _ = w.Write([]byte(`{"id":"a","cash":"1","equity":"1","buying_power":"1","portfolio_value":"1"}`))
		case r.URL.Path == "/v2/orders" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"oid","client_order_id":"1","symbol":"AAPL","side":"buy","qty":"1","filled_qty":"0","filled_avg_price":"0","status":"accepted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	inner := &broker.Alpaca{
		BaseURL: srv.URL,
		Key:     "k",
		Secret:  "s",
		HTTP:    srv.Client(),
	}
	cached := broker.NewCachedClient(inner, time.Minute)

	if _, err := cached.GetAccount(context.Background()); err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if _, err := cached.SubmitOrder(context.Background(), broker.OrderRequest{
		Symbol: "AAPL", Side: "buy", Qty: 1, ClientOrderID: "1", TimeInForce: "day", Type: "market",
	}); err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if _, err := cached.GetAccount(context.Background()); err != nil {
		t.Fatalf("GetAccount after submit: %v", err)
	}
	if accountCalls.Load() != 2 {
		t.Fatalf("expected cache invalidate after SubmitOrder, got %d account calls", accountCalls.Load())
	}
}
