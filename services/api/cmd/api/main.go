package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cyh/stock-agents/services/api/internal/agentsclient"
	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/broker"
	"github.com/cyh/stock-agents/services/api/internal/config"
	"github.com/cyh/stock-agents/services/api/internal/db"
	"github.com/cyh/stock-agents/services/api/internal/httpserver"
	"github.com/cyh/stock-agents/services/api/internal/ledger"
	"github.com/cyh/stock-agents/services/api/internal/risk"
	"github.com/cyh/stock-agents/services/api/internal/scheduler"
	"github.com/cyh/stock-agents/services/api/internal/strategy"
	"github.com/cyh/stock-agents/services/api/internal/workflow"
	"github.com/redis/go-redis/v9"
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

	ledgerSvc := &ledger.Service{DB: gormDB}
	approvalsSvc := &approvals.Service{DB: gormDB, Ledger: ledgerSvc}

	var eodRunner httpserver.EODRunner
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "redis url: %v\n", err)
			os.Exit(1)
		}
		rdb := redis.NewClient(opt)
		eodRunner = &workflow.Runner{
			DB: gormDB,
			Agents: &agentsclient.Client{
				DataURL:      cfg.AgentDataURL,
				ResearchURL:  cfg.AgentResearchURL,
				DecisionURL:  cfg.AgentDecisionURL,
				PortfolioURL: cfg.AgentPortfolioURL,
				RiskURL:      cfg.AgentRiskURL,
			},
			Ledger: ledgerSvc,
			Risk: risk.LoadEngineFromMap(map[string]float64{
				"max_order_notional":     cfg.RiskMaxOrderNotional,
				"max_single_name_weight": cfg.RiskMaxSingleNameWeight,
				"min_cash_ratio":         cfg.RiskMinCashRatio,
			}),
			Redis: rdb,
		}
	}

	strategySvc := &strategy.Service{DB: gormDB}
	var schedReloader httpserver.SchedulerReloader = httpserver.NoopSchedulerReloader{}

	if eodRunner != nil {
		sched, err := scheduler.New(scheduler.Options{
			Runner:   eodRunner,
			Location: scheduler.NewYorkLocation(),
			Source:   strategySvc,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "scheduler: %v\n", err)
			os.Exit(1)
		}
		schedReloader = sched
		go func() {
			if err := sched.Start(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "scheduler: %v\n", err)
			}
		}()
	}

	var brokerClient broker.Client
	if alpaca, err := broker.NewAlpaca(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "broker: %v (continuing without Alpaca client)\n", err)
	} else {
		brokerClient = broker.NewCachedClient(alpaca, 5*time.Second)
	}

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:         gormDB,
		JWTSecret:  cfg.JWTSecret,
		Runner:     eodRunner,
		Approvals:  approvalsSvc,
		Ledger:     ledgerSvc,
		Config:     cfg,
		Strategies: strategySvc,
		Scheduler:  schedReloader,
		Broker:     brokerClient,
	})

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if err := router.Run(addr); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
