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
	"github.com/cyh/stock-agents/services/api/internal/stream"
	"github.com/cyh/stock-agents/services/api/internal/symbolsearch"
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

	var brokerClient broker.Client
	if alpaca, err := broker.NewAlpaca(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "broker: %v (continuing without Alpaca client)\n", err)
	} else {
		brokerClient = broker.NewCachedClient(alpaca, 5*time.Second)
	}

	approvalsSvc := &approvals.Service{DB: gormDB, Ledger: ledgerSvc, Broker: brokerClient}

	var workflowRunner httpserver.WorkflowRunner
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "redis url: %v\n", err)
			os.Exit(1)
		}
		rdb := redis.NewClient(opt)
		workflowRunner = &workflow.Runner{
			DB: gormDB,
			Agents: &agentsclient.Client{
				RuntimeURL:   cfg.AgentRuntimeURL,
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
			Redis:  rdb,
			Broker: brokerClient,
			Config: cfg,
		}
	}

	strategySvc := &strategy.Service{DB: gormDB}
	var schedReloader httpserver.SchedulerReloader = httpserver.NoopSchedulerReloader{}

	if workflowRunner != nil {
		sched, err := scheduler.New(scheduler.Options{
			Runner:   workflowRunner,
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

	streamHub := stream.NewHub(cfg.AlpacaStreamEnabled, cfg.AlpacaAPIKey, cfg.AlpacaAPISecret)
	if cfg.AlpacaStreamEnabled {
		if err := streamHub.Start(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "stream hub: %v\n", err)
		}
	}

	symbolSearcher := symbolsearch.NewFromConfig(cfg, nil)

	router := httpserver.NewRouter(httpserver.RouterDeps{
		DB:           gormDB,
		JWTSecret:    cfg.JWTSecret,
		Runner:       workflowRunner,
		Approvals:    approvalsSvc,
		Ledger:       ledgerSvc,
		Config:       cfg,
		Strategies:   strategySvc,
		Scheduler:    schedReloader,
		Broker:       brokerClient,
		Stream:       streamHub,
		SymbolSearch: symbolSearcher,
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
