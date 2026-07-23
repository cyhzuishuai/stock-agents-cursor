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
