package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAutoMigrateAllModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	err = db.AutoMigrate(
		&User{},
		&Account{},
		&Position{},
		&Order{},
		&WatchlistSymbol{},
		&WorkflowRun{},
		&WorkflowStepResult{},
		&TradeProposal{},
		&Approval{},
		&RiskRuleConfig{},
		&NavSnapshot{},
	)
	if err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
}
