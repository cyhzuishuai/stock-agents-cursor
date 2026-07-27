package db

import (
	"github.com/cyh/stock-agents/services/api/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Connect opens a Postgres database using the given DSN.
func Connect(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

// ConnectSQLite opens a SQLite database (CGO-less via glebarez) for tests.
func ConnectSQLite(dsn string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.Account{},
		&models.Position{},
		&models.Order{},
		&models.WatchlistSymbol{},
		&models.WorkflowRun{},
		&models.WorkflowStepResult{},
		&models.TradeProposal{},
		&models.Approval{},
		&models.RiskRuleConfig{},
		&models.NavSnapshot{},
		&models.Strategy{},
	)
}
