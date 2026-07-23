package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL              string
	RedisURL                 string
	JWTSecret                string
	AdminUsername            string
	AdminPassword            string
	InitialCash              float64
	Watchlist                []string
	RiskMaxOrderNotional     float64
	RiskMaxSingleNameWeight  float64
	RiskMinCashRatio         float64
	AgentDataURL             string
	AgentResearchURL         string
	AgentDecisionURL         string
	AgentPortfolioURL        string
	AgentRiskURL             string
	InternalEODToken         string
	MarketDataProvider       string
}

func Load() (*Config, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	initialCash, err := parseFloatWithDefault("INITIAL_CASH", 100000)
	if err != nil {
		return nil, err
	}

	riskMaxOrderNotional, err := parseFloatWithDefault("RISK_MAX_ORDER_NOTIONAL", 10000)
	if err != nil {
		return nil, err
	}

	riskMaxSingleNameWeight, err := parseFloatWithDefault("RISK_MAX_SINGLE_NAME_WEIGHT", 0.20)
	if err != nil {
		return nil, err
	}

	riskMinCashRatio, err := parseFloatWithDefault("RISK_MIN_CASH_RATIO", 0.10)
	if err != nil {
		return nil, err
	}

	watchlist := parseWatchlist(os.Getenv("WATCHLIST"))

	return &Config{
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		RedisURL:                os.Getenv("REDIS_URL"),
		JWTSecret:               jwtSecret,
		AdminUsername:           os.Getenv("ADMIN_USERNAME"),
		AdminPassword:           os.Getenv("ADMIN_PASSWORD"),
		InitialCash:             initialCash,
		Watchlist:               watchlist,
		RiskMaxOrderNotional:    riskMaxOrderNotional,
		RiskMaxSingleNameWeight: riskMaxSingleNameWeight,
		RiskMinCashRatio:        riskMinCashRatio,
		AgentDataURL:            os.Getenv("AGENT_DATA_URL"),
		AgentResearchURL:        os.Getenv("AGENT_RESEARCH_URL"),
		AgentDecisionURL:        os.Getenv("AGENT_DECISION_URL"),
		AgentPortfolioURL:       os.Getenv("AGENT_PORTFOLIO_URL"),
		AgentRiskURL:            os.Getenv("AGENT_RISK_URL"),
		InternalEODToken:        os.Getenv("INTERNAL_EOD_TOKEN"),
		MarketDataProvider:      os.Getenv("MARKET_DATA_PROVIDER"),
	}, nil
}

func parseFloatWithDefault(key string, defaultValue float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}

	return value, nil
}

func parseWatchlist(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	watchlist := make([]string, 0, len(parts))
	for _, part := range parts {
		symbol := strings.TrimSpace(part)
		if symbol != "" {
			watchlist = append(watchlist, symbol)
		}
	}

	return watchlist
}
