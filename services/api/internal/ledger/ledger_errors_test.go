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

func TestApplyFillInsufficientCash(t *testing.T) {
	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	account := models.Account{Currency: "USD", Cash: 500, InitialCapital: 500}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	svc := &ledger.Service{DB: gormDB}
	_, err = svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "buy",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	})
	if !errors.Is(err, ledger.ErrInsufficientCash) {
		t.Fatalf("ApplyFill: got %v want ErrInsufficientCash", err)
	}

	var updated models.Account
	if err := gormDB.First(&updated, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if updated.Cash != 500 {
		t.Fatalf("cash unchanged: got %v want 500", updated.Cash)
	}
}

func TestApplyFillInsufficientQty(t *testing.T) {
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

	_, err = svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Side:      "sell",
		Qty:       15,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	})
	if !errors.Is(err, ledger.ErrInsufficientQty) {
		t.Fatalf("ApplyFill: got %v want ErrInsufficientQty", err)
	}

	var pos models.Position
	if err := gormDB.Where("account_id = ? AND symbol = ?", account.ID, "AAPL").First(&pos).Error; err != nil {
		t.Fatalf("find position: %v", err)
	}
	if pos.Qty != 10 {
		t.Fatalf("position qty unchanged: got %v want 10", pos.Qty)
	}
}

func TestApplyFillInvalidSide(t *testing.T) {
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
		Side:      "short",
		Qty:       10,
		FillPrice: 100,
		TradeDate: "2026-07-23",
	})
	if !errors.Is(err, ledger.ErrInvalidSide) {
		t.Fatalf("ApplyFill: got %v want ErrInvalidSide", err)
	}
}
