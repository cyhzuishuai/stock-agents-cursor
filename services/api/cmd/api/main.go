package main

import (
	"fmt"
	"os"

	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// DB migrate/seed wired in Task 02.5; auth routes registered with nil DB for now.
	router := httpserver.NewRouter(nil, cfg.JWTSecret)

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if err := router.Run(addr); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
