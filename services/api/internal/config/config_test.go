package config_test

import (
	"os"
	"testing"

	"github.com/cyh/stock-agents/services/api/internal/config"
)

func TestLoadRequiresJWTSecret(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://x")
	os.Setenv("REDIS_URL", "redis://x")
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET missing")
	}
}

func TestLoadAlpacaConfig(t *testing.T) {
	os.Clearenv()
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("ALPACA_API_KEY", "k")
	os.Setenv("ALPACA_API_SECRET", "s")
	os.Setenv("ALPACA_BASE_URL", "https://paper-api.alpaca.markets")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AlpacaAPIKey != "k" {
		t.Fatalf("AlpacaAPIKey: got %q want %q", cfg.AlpacaAPIKey, "k")
	}
	if cfg.AlpacaAPISecret != "s" {
		t.Fatalf("AlpacaAPISecret: got %q want %q", cfg.AlpacaAPISecret, "s")
	}
	if cfg.AlpacaBaseURL != "https://paper-api.alpaca.markets" {
		t.Fatalf("AlpacaBaseURL: got %q", cfg.AlpacaBaseURL)
	}
	if cfg.AlpacaStreamEnabled {
		t.Fatal("AlpacaStreamEnabled should default to false")
	}
}

func TestLoadAgentRuntimeURL(t *testing.T) {
	os.Clearenv()
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("AGENT_RUNTIME_URL", "http://agent-runtime:8001")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentRuntimeURL != "http://agent-runtime:8001" {
		t.Fatalf("AgentRuntimeURL: got %q", cfg.AgentRuntimeURL)
	}
}

func TestLoadAlpacaBaseURLDefault(t *testing.T) {
	os.Clearenv()
	os.Setenv("JWT_SECRET", "test-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AlpacaBaseURL != "https://paper-api.alpaca.markets" {
		t.Fatalf("AlpacaBaseURL default: got %q", cfg.AlpacaBaseURL)
	}
}

func TestLoadAlpacaStreamEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"yes", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			os.Clearenv()
			os.Setenv("JWT_SECRET", "test-secret")
			if tc.raw != "" {
				os.Setenv("ALPACA_STREAM_ENABLED", tc.raw)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.AlpacaStreamEnabled != tc.want {
				t.Fatalf("AlpacaStreamEnabled for %q: got %v want %v", tc.raw, cfg.AlpacaStreamEnabled, tc.want)
			}
		})
	}
}
