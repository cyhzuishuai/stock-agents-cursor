package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
)

func TestApplyFillSell(t *testing.T) {
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
	if _, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "buy",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	}); err != nil {
		t.Fatalf("buy ApplyFill: %v", err)
	}

	order, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "sell",
		Qty:       5,
		FillPrice: 110,
		TradeDate: "2026-07-23",
	})
	if err != nil {
		t.Fatalf("sell ApplyFill: %v", err)
	}

	var updated models.Account
	if err := gormDB.First(&updated, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if updated.Cash != 99550 {
		t.Fatalf("cash: got %v want 99550", updated.Cash)
	}

	var pos models.Position
	if err := gormDB.Where("account_id = ? AND symbol = ?", account.ID, "AAPL").First(&pos).Error; err != nil {
		t.Fatalf("find position: %v", err)
	}
	if pos.Qty != 5 || pos.AvgCost != 100 {
		t.Fatalf("position: got qty=%v avg=%v want qty=5 avg=100", pos.Qty, pos.AvgCost)
	}

	if order.Status != "filled" || order.Notional != 550 {
		t.Fatalf("order: got status=%q notional=%v want filled/550", order.Status, order.Notional)
	}
}

func TestApplyFillSellAllDeletesPosition(t *testing.T) {
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
	if _, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "buy",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	}); err != nil {
		t.Fatalf("buy ApplyFill: %v", err)
	}

	if _, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "sell",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	}); err != nil {
		t.Fatalf("sell ApplyFill: %v", err)
	}

	var updated models.Account
	if err := gormDB.First(&updated, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if updated.Cash != 100000 {
		t.Fatalf("cash: got %v want 100000", updated.Cash)
	}

	var count int64
	if err := gormDB.Model(&models.Position{}).
		Where("account_id = ? AND symbol = ?", account.ID, "AAPL").
		Count(&count).Error; err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if count != 0 {
		t.Fatalf("position row count: got %v want 0", count)
	}
}

func TestApplyFillSecondBuyWeightedAvgCost(t *testing.T) {
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
	for _, fill := range []ledger.FillRequest{
		{AccountID: account.ID, Symbol: "AAPL", Side: "buy", Qty: 10, FillPrice: 100, TradeDate: "2026-07-23"},
		{AccountID: account.ID, Symbol: "AAPL", Side: "buy", Qty: 10, FillPrice: 120, TradeDate: "2026-07-23"},
	} {
		if _, err := svc.ApplyFill(context.Background(), fill); err != nil {
			t.Fatalf("ApplyFill: %v", err)
		}
	}

	var pos models.Position
	if err := gormDB.Where("account_id = ? AND symbol = ?", account.ID, "AAPL").First(&pos).Error; err != nil {
		t.Fatalf("find position: %v", err)
	}
	if pos.Qty != 20 || pos.AvgCost != 110 {
		t.Fatalf("position: got qty=%v avg=%v want qty=20 avg=110", pos.Qty, pos.AvgCost)
	}
}

func TestApplyFillInsufficientQtyNoPosition(t *testing.T) {
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
	_, err = svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "sell",
		Qty:       1,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	})
	if !errors.Is(err, ledger.ErrInsufficientQty) {
		t.Fatalf("ApplyFill: got %v want ErrInsufficientQty", err)
	}
}
