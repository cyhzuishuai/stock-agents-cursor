package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

func setupLedgerTest(t *testing.T) (*ledger.Service, *gorm.DB) {
	t.Helper()
	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return &ledger.Service{DB: gormDB}, gormDB
}

func TestUpsertNAV(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	account := models.Account{Currency: "USD", Cash: 50000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Qty:       10,
		AvgCost:   100,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID,
		Symbol:    "MSFT",
		Qty:       5,
		AvgCost:   200,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}

	snap, err := svc.UpsertNAV(context.Background(), "2026-07-23", map[string]float64{
		"AAPL": 110,
		"MSFT": 210,
	})
	if err != nil {
		t.Fatalf("UpsertNAV: %v", err)
	}

	wantEquity := 10.0*110 + 5.0*210 // 2150
	wantNav := 50000.0 + wantEquity
	if snap.TradeDate != "2026-07-23" {
		t.Fatalf("trade_date: got %q want 2026-07-23", snap.TradeDate)
	}
	if snap.Cash != 50000 {
		t.Fatalf("cash: got %v want 50000", snap.Cash)
	}
	if snap.Equity != wantEquity {
		t.Fatalf("equity: got %v want %v", snap.Equity, wantEquity)
	}
	if snap.Nav != wantNav {
		t.Fatalf("nav: got %v want %v", snap.Nav, wantNav)
	}

	snap2, err := svc.UpsertNAV(context.Background(), "2026-07-23", map[string]float64{
		"AAPL": 120,
		"MSFT": 200,
	})
	if err != nil {
		t.Fatalf("UpsertNAV second call: %v", err)
	}
	if snap2.ID != snap.ID {
		t.Fatalf("expected upsert same row, got id %v want %v", snap2.ID, snap.ID)
	}
	wantEquity2 := 10.0*120 + 5.0*200
	if snap2.Equity != wantEquity2 {
		t.Fatalf("updated equity: got %v want %v", snap2.Equity, wantEquity2)
	}
	if snap2.Nav != 50000.0+wantEquity2 {
		t.Fatalf("updated nav: got %v want %v", snap2.Nav, 50000+wantEquity2)
	}

	var count int64
	if err := gormDB.Model(&models.NavSnapshot{}).Count(&count).Error; err != nil {
		t.Fatalf("count nav snapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("nav snapshot count: got %v want 1", count)
	}
}

func TestUpsertNAVMissingMark(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	account := models.Account{Currency: "USD", Cash: 100000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID,
		Symbol:    "AAPL",
		Qty:       10,
		AvgCost:   100,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}

	_, err := svc.UpsertNAV(context.Background(), "2026-07-23", map[string]float64{})
	if !errors.Is(err, ledger.ErrMissingMark) {
		t.Fatalf("UpsertNAV: got %v want ErrMissingMark", err)
	}
}

func TestApplyFillStopTake(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	account := models.Account{Currency: "USD", Cash: 100000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	stop := 90.0
	take := 130.0
	_, err := svc.ApplyFill(context.Background(), ledger.FillRequest{
		AccountID:  account.ID,
		Symbol:     "AAPL",
		Side:       "buy",
		Qty:        10,
		FillPrice:  100,
		TradeDate:  "2026-07-23",
		StopLoss:   &stop,
		TakeProfit: &take,
	})
	if err != nil {
		t.Fatalf("ApplyFill: %v", err)
	}

	var pos models.Position
	if err := gormDB.Where("account_id = ? AND symbol = ?", account.ID, "AAPL").First(&pos).Error; err != nil {
		t.Fatalf("find position: %v", err)
	}
	if pos.StopLoss == nil || *pos.StopLoss != 90 {
		t.Fatalf("stop_loss: got %v want 90", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 130 {
		t.Fatalf("take_profit: got %v want 130", pos.TakeProfit)
	}
}
