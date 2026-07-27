package db_test

import (
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestSeedCreatesAdminAndAccount(t *testing.T) {
	gormDB, err := db.ConnectSQLite("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("ConnectSQLite: %v", err)
	}
	if err := db.AutoMigrate(gormDB); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	cfg := &config.Config{
		AdminUsername:           "admin",
		AdminPassword:           "admin123",
		InitialCash:             100000,
		Watchlist:               []string{"AAPL", "MSFT"},
		RiskMaxOrderNotional:    10000,
		RiskMaxSingleNameWeight: 0.20,
		RiskMinCashRatio:        0.10,
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	var user models.User
	if err := gormDB.Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("find admin user: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("admin123")); err != nil {
		t.Fatalf("password hash: %v", err)
	}

	var account models.Account
	if err := gormDB.First(&account).Error; err != nil {
		t.Fatalf("find account: %v", err)
	}
	if account.Cash != 100000 || account.InitialCapital != 100000 {
		t.Fatalf("account cash: got cash=%v initial=%v want 100000", account.Cash, account.InitialCapital)
	}
	if account.Currency != "USD" {
		t.Fatalf("account currency: got %q want USD", account.Currency)
	}

	var watchlist []models.WatchlistSymbol
	if err := gormDB.Find(&watchlist).Error; err != nil {
		t.Fatalf("find watchlist: %v", err)
	}
	if len(watchlist) != 2 {
		t.Fatalf("watchlist count: got %d want 2", len(watchlist))
	}

	riskKeys := map[string]float64{
		"max_order_notional":     10000,
		"max_single_name_weight": 0.20,
		"min_cash_ratio":         0.10,
	}
	for key, want := range riskKeys {
		var rule models.RiskRuleConfig
		if err := gormDB.Where("key = ?", key).First(&rule).Error; err != nil {
			t.Fatalf("find risk key %q: %v", key, err)
		}
		if rule.ValueFloat != want {
			t.Fatalf("risk %q: got %v want %v", key, rule.ValueFloat, want)
		}
	}

	var strategy models.Strategy
	if err := gormDB.Where("name = ?", "整体策略1").First(&strategy).Error; err != nil {
		t.Fatalf("find default strategy: %v", err)
	}
	if !strategy.IsSystemDefault {
		t.Fatalf("strategy is_system_default: got false want true")
	}
	if !strategy.IsActive {
		t.Fatalf("strategy is_active: got false want true")
	}
	if strategy.PreOpenMinutes != 10 {
		t.Fatalf("strategy pre_open_minutes: got %d want 10", strategy.PreOpenMinutes)
	}
	if strategy.IntradayEveryMinutes != 60 {
		t.Fatalf("strategy intraday_every_minutes: got %d want 60", strategy.IntradayEveryMinutes)
	}
	if strategy.IntradayStartET != "10:00" || strategy.IntradayEndET != "15:00" {
		t.Fatalf("strategy intraday window: got %s–%s want 10:00–15:00", strategy.IntradayStartET, strategy.IntradayEndET)
	}
	if strategy.ExecutionMode != "auto_reject_breaches" {
		t.Fatalf("strategy execution_mode: got %q want auto_reject_breaches", strategy.ExecutionMode)
	}
}
