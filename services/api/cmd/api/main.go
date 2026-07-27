package main

import (
	"fmt"
	"os"

	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	if cfg.DatabaseURL == "" {
		fmt.Fprintf(os.Stderr, "config: DATABASE_URL is required\n")
		os.Exit(1)
	}

	gormDB, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db connect: %v\n", err)
		os.Exit(1)
	}

	if err := db.AutoMigrate(gormDB); err != nil {
		fmt.Fprintf(os.Stderr, "db migrate: %v\n", err)
		os.Exit(1)
	}

	if err := db.Seed(gormDB, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "db seed: %v\n", err)
		os.Exit(1)
	}

	router := httpserver.NewRouter(gormDB, cfg.JWTSecret)

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if err := router.Run(addr); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
