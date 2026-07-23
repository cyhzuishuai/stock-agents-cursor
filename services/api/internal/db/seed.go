package db

import (
	"fmt"

	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Seed bootstraps admin user, paper account, watchlist, and risk rule configs.
// User and account are created only when none exist (idempotent bootstrap).
func Seed(db *gorm.DB, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	username := cfg.AdminUsername
	if username == "" {
		username = "admin"
	}
	password := cfg.AdminPassword
	if password == "" {
		password = "admin123"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&models.User{}).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount == 0 {
			if err := tx.Create(&models.User{
				Username:     username,
				PasswordHash: string(hash),
			}).Error; err != nil {
				return err
			}
		}

		var accountCount int64
		if err := tx.Model(&models.Account{}).Count(&accountCount).Error; err != nil {
			return err
		}
		if accountCount == 0 {
			if err := tx.Create(&models.Account{
				Currency:       "USD",
				Cash:           cfg.InitialCash,
				InitialCapital: cfg.InitialCash,
			}).Error; err != nil {
				return err
			}
		}

		for _, symbol := range cfg.Watchlist {
			if err := tx.Where(models.WatchlistSymbol{Symbol: symbol}).
				FirstOrCreate(&models.WatchlistSymbol{Symbol: symbol}).Error; err != nil {
				return err
			}
		}

		riskRules := []models.RiskRuleConfig{
			{Key: "max_order_notional", ValueFloat: cfg.RiskMaxOrderNotional},
			{Key: "max_single_name_weight", ValueFloat: cfg.RiskMaxSingleNameWeight},
			{Key: "min_cash_ratio", ValueFloat: cfg.RiskMinCashRatio},
		}
		for _, rule := range riskRules {
			if err := tx.Where(models.RiskRuleConfig{Key: rule.Key}).
				Assign(models.RiskRuleConfig{ValueFloat: rule.ValueFloat}).
				FirstOrCreate(&rule).Error; err != nil {
				return err
			}
		}

		return nil
	})
}
