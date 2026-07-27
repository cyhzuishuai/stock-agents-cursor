package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

func TestAccountSnapshotShape(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	stop := 160.0
	take := 220.0
	account := models.Account{Currency: "USD", Cash: 100000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID:  account.ID,
		Symbol:     "AAPL",
		Qty:        10,
		AvgCost:    180,
		StopLoss:   &stop,
		TakeProfit: &take,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}

	snap, err := svc.AccountSnapshot(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("AccountSnapshot: %v", err)
	}

	if snap.Cash != 100000 {
		t.Fatalf("cash: got %v want 100000", snap.Cash)
	}
	if snap.Currency != "USD" {
		t.Fatalf("currency: got %q want USD", snap.Currency)
	}
	if len(snap.Positions) != 1 {
		t.Fatalf("positions len: got %d want 1", len(snap.Positions))
	}
	pos := snap.Positions[0]
	if pos.Symbol != "AAPL" || pos.Qty != 10 || pos.AvgCost != 180 {
		t.Fatalf("position: got %+v", pos)
	}
	if pos.StopLoss == nil || *pos.StopLoss != 160 {
		t.Fatalf("stop_loss: got %v want 160", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 220 {
		t.Fatalf("take_profit: got %v want 220", pos.TakeProfit)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	for _, key := range []string{"cash", "currency", "positions"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing top-level key %q in %s", key, raw)
		}
	}

	var positions []map[string]json.RawMessage
	if err := json.Unmarshal(doc["positions"], &positions); err != nil {
		t.Fatalf("unmarshal positions: %v", err)
	}
	posDoc := positions[0]
	for _, key := range []string{"symbol", "qty", "avg_cost", "stop_loss", "take_profit"} {
		if _, ok := posDoc[key]; !ok {
			t.Fatalf("missing position key %q in %s", key, raw)
		}
	}
}

func TestAccountSnapshotEmptyPositions(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	account := models.Account{Currency: "USD", Cash: 50000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	snap, err := svc.AccountSnapshot(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("AccountSnapshot: %v", err)
	}
	if snap.Cash != 50000 {
		t.Fatalf("cash: got %v want 50000", snap.Cash)
	}
	if snap.Positions == nil || len(snap.Positions) != 0 {
		t.Fatalf("positions: got %v want empty slice", snap.Positions)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"cash":50000,"currency":"USD","positions":[]}` {
		t.Fatalf("json: got %s", raw)
	}
}

func TestAccountSnapshotNotFound(t *testing.T) {
	svc, _ := setupLedgerTest(t)

	_, err := svc.AccountSnapshot(context.Background(), 999)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("AccountSnapshot: got %v want ErrRecordNotFound", err)
	}
}

func TestAccountSnapshotNullStopTake(t *testing.T) {
	svc, gormDB := setupLedgerTest(t)

	account := models.Account{Currency: "USD", Cash: 100000, InitialCapital: 100000}
	if err := gormDB.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := gormDB.Create(&models.Position{
		AccountID: account.ID,
		Symbol:    "MSFT",
		Qty:       5,
		AvgCost:   400,
	}).Error; err != nil {
		t.Fatalf("create position: %v", err)
	}

	snap, err := svc.AccountSnapshot(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("AccountSnapshot: %v", err)
	}
	pos := snap.Positions[0]
	if pos.StopLoss != nil || pos.TakeProfit != nil {
		t.Fatalf("expected nil stop/take, got stop=%v take=%v", pos.StopLoss, pos.TakeProfit)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	posMap := doc["positions"].([]any)[0].(map[string]any)
	if posMap["stop_loss"] != nil {
		t.Fatalf("stop_loss: got %v want null", posMap["stop_loss"])
	}
	if posMap["take_profit"] != nil {
		t.Fatalf("take_profit: got %v want null", posMap["take_profit"])
	}
}
