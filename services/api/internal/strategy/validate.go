package strategy

import (
	"fmt"
	"strings"

	"github.com/cyh/stock-agents/services/api/internal/models"
)

const (
	ExecutionModeAutoReject      = "auto_reject_breaches"
	ExecutionModeRequireApproval = "require_approval"
)

func ValidateStrategyFields(s models.Strategy) error {
	switch s.ExecutionMode {
	case ExecutionModeAutoReject, ExecutionModeRequireApproval:
	default:
		return fmt.Errorf("invalid execution_mode %q", s.ExecutionMode)
	}

	if s.PreOpenMinutes < 0 {
		return fmt.Errorf("pre_open_minutes must be >= 0")
	}
	if s.PreOpenMinutes > usRegularOpenMinutes {
		return fmt.Errorf("pre_open_minutes exceeds market open offset")
	}

	if s.IntradayEveryMinutes < 0 {
		return fmt.Errorf("intraday_every_minutes must be >= 0")
	}
	if s.IntradayEveryMinutes > 0 && s.IntradayEveryMinutes < 15 {
		return fmt.Errorf("intraday_every_minutes must be 0 or >= 15")
	}

	start, err := parseHHMM(s.IntradayStartET)
	if err != nil {
		return fmt.Errorf("invalid intraday_start_et: %w", err)
	}
	end, err := parseHHMM(s.IntradayEndET)
	if err != nil {
		return fmt.Errorf("invalid intraday_end_et: %w", err)
	}
	if start > end {
		return fmt.Errorf("intraday_start_et must be <= intraday_end_et")
	}

	return nil
}

func parseHHMM(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM, got %q", value)
	}
	var hour, minute int
	if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("invalid hour in %q", value)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minute); err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid minute in %q", value)
	}
	return hour*60 + minute, nil
}
