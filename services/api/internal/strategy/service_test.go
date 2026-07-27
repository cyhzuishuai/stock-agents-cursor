package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/models"
	"gorm.io/gorm"
)

func setupService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gormDB, err := db.ConnectSQLite(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
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
		Watchlist:               []string{"AAPL"},
		RiskMaxOrderNotional:    10000,
		RiskMaxSingleNameWeight: 0.20,
		RiskMinCashRatio:        0.10,
	}
	if err := db.Seed(gormDB, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return &Service{DB: gormDB}, gormDB
}

func validCreateInput(name string) CreateInput {
	s := validStrategy()
	return CreateInput{
		Name:                 name,
		Description:          "test strategy",
		PreOpenMinutes:       s.PreOpenMinutes,
		IntradayEveryMinutes: s.IntradayEveryMinutes,
		IntradayStartET:      s.IntradayStartET,
		IntradayEndET:        s.IntradayEndET,
		ExecutionMode:        s.ExecutionMode,
	}
}

func TestCreateThenListIncludesRow(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, validCreateInput("Custom Strategy"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.IsActive {
		t.Fatal("created strategy should not be active")
	}
	if created.IsSystemDefault {
		t.Fatal("created strategy should not be system default")
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, s := range list {
		if s.ID == created.ID {
			found = true
			if s.IsActive || s.IsSystemDefault {
				t.Fatalf("listed row flags: active=%v system=%v", s.IsActive, s.IsSystemDefault)
			}
		}
	}
	if !found {
		t.Fatal("created strategy not found in List")
	}
}

func TestActivateOnlyOneActive(t *testing.T) {
	svc, gormDB := setupService(t)
	ctx := context.Background()

	a, err := svc.Create(ctx, validCreateInput("Strategy A"))
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	b, err := svc.Create(ctx, validCreateInput("Strategy B"))
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	if _, err := svc.Activate(ctx, a.ID); err != nil {
		t.Fatalf("Activate A: %v", err)
	}
	if _, err := svc.Activate(ctx, b.ID); err != nil {
		t.Fatalf("Activate B: %v", err)
	}

	var strategies []models.Strategy
	if err := gormDB.Find(&strategies).Error; err != nil {
		t.Fatalf("Find: %v", err)
	}
	activeCount := 0
	for _, s := range strategies {
		if s.IsActive {
			activeCount++
			if s.ID != b.ID {
				t.Fatalf("unexpected active strategy id=%d name=%q", s.ID, s.Name)
			}
		}
	}
	if activeCount != 1 {
		t.Fatalf("active count: got %d want 1", activeCount)
	}
}

func TestDeleteSystemDefaultForbidden(t *testing.T) {
	svc, gormDB := setupService(t)
	ctx := context.Background()

	var def models.Strategy
	if err := gormDB.Where("is_system_default = ?", true).First(&def).Error; err != nil {
		t.Fatalf("find system default: %v", err)
	}

	err := svc.Delete(ctx, def.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Delete system default: got %v want ErrForbidden", err)
	}
}

func TestDeleteActiveForbidden(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, validCreateInput("Active To Delete"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Activate(ctx, created.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	err = svc.Delete(ctx, created.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Delete active: got %v want ErrForbidden", err)
	}
}

func TestUpdateInvalidModeValidationError(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, validCreateInput("Update Target"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	in := validCreateInput("Update Target")
	in.ExecutionMode = "invalid"
	_, err = svc.Update(ctx, created.ID, UpdateInput(in))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Update invalid mode: got %v want ErrValidation", err)
	}
}
