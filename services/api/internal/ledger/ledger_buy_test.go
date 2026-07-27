package ledger_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
)

func TestApplyFillBuy(t *testing.T) {
	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	account := models.Account{Currency: "USD", Cash: 100000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	svc := &ledger.Service{DB: gormDB}
	order, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "buy",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	})
	if err != nil {
		t.Fatalf("ApplyFill: %v", err)
	}

	var updated models.Account
	if err := gormDB.First(&updated, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if updated.Cash != 99000 {
		t.Fatalf("cash: got %v want 99000", updated.Cash)
	}

	var pos models.Position
	if err := gormDB.Where("account_id = ? AND symbol = ?", account.ID, "AAPL").First(&pos).Error; err != nil {
		t.Fatalf("find position: %v", err)
	}
	if pos.Qty != 10 || pos.AvgCost != 100 {
		t.Fatalf("position: got qty=%v avg=%v want qty=10 avg=100", pos.Qty, pos.AvgCost)
	}

	if order.Status != "filled" {
		t.Fatalf("order status: got %q want filled", order.Status)
	}
	if order.Notional != 1000 || order.FillPrice != 100 || order.Qty != 10 {
		t.Fatalf("order: got notional=%v fill=%v qty=%v want 1000/100/10", order.Notional, order.FillPrice, order.Qty)
	}
}
