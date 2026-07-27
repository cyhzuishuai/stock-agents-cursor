package strategy

import (
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

func validStrategy() models.Strategy {
	return models.Strategy{
		Name:                 "Test Strategy",
		PreOpenMinutes:       10,
		IntradayEveryMinutes: 60,
		IntradayStartET:      "10:00",
		IntradayEndET:        "15:00",
		ExecutionMode:        ExecutionModeAutoReject,
	}
}

func TestValidateStrategyFieldsValid(t *testing.T) {
	if err := ValidateStrategyFields(validStrategy()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStrategyFieldsEmptyName(t *testing.T) {
	for _, name := range []string{"", " ", "\t"} {
		s := validStrategy()
		s.Name = name
		if err := ValidateStrategyFields(s); err == nil {
			t.Fatalf("expected error for name %q", name)
		}
	}
}

func TestValidateStrategyFieldsInvalidExecutionMode(t *testing.T) {
	s := validStrategy()
	s.ExecutionMode = "invalid"
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error for invalid execution_mode")
	}
}

func TestValidateBypassRiskOK(t *testing.T) {
	s := validStrategy()
	s.ExecutionMode = ExecutionModeBypassRisk
	if err := ValidateStrategyFields(s); err != nil {
		t.Fatal(err)
	}
}

func TestValidateStrategyFieldsNegativePreOpen(t *testing.T) {
	s := validStrategy()
	s.PreOpenMinutes = -1
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error for negative pre_open_minutes")
	}
}

func TestValidateStrategyFieldsPreOpenExceedsMarketOpen(t *testing.T) {
	s := validStrategy()
	s.PreOpenMinutes = 571
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error for pre_open_minutes > 570")
	}
}

func TestValidateStrategyFieldsPreOpenAtMarketOpenLimit(t *testing.T) {
	s := validStrategy()
	s.PreOpenMinutes = 570
	if err := ValidateStrategyFields(s); err != nil {
		t.Fatalf("pre_open_minutes=570 should be valid: %v", err)
	}
}

func TestValidateStrategyFieldsIntradayIntervalTooSmall(t *testing.T) {
	s := validStrategy()
	s.IntradayEveryMinutes = 10
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error for intraday_every_minutes < 15")
	}
}

func TestValidateStrategyFieldsIntradayZeroAllowed(t *testing.T) {
	s := validStrategy()
	s.IntradayEveryMinutes = 0
	if err := ValidateStrategyFields(s); err != nil {
		t.Fatalf("intraday_every_minutes=0 should be valid: %v", err)
	}
}

func TestValidateStrategyFieldsStartAfterEnd(t *testing.T) {
	s := validStrategy()
	s.IntradayStartET = "16:00"
	s.IntradayEndET = "15:00"
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error when start > end")
	}
}

func TestValidateStrategyFieldsInvalidTimeFormat(t *testing.T) {
	s := validStrategy()
	s.IntradayStartET = "25:00"
	if err := ValidateStrategyFields(s); err == nil {
		t.Fatal("expected error for invalid HH:MM")
	}
}
