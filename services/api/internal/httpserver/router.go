package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/auth"
	"github.com/gin-gonic/gin"
)

func corsMiddleware() gin.HandlerFunc {
	const webOrigin = "http://localhost:3000"
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin == webOrigin {
			c.Header("Access-Control-Allow-Origin", webOrigin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func NewRouter(deps RouterDeps) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := &API{
		DB:           deps.DB,
		JWTSecret:    deps.JWTSecret,
		Runner:       deps.Runner,
		Approvals:    deps.Approvals,
		Ledger:       deps.Ledger,
		Config:       deps.Config,
		Strategies:   deps.Strategies,
		Scheduler:    deps.Scheduler,
		HTTPClient:   deps.HTTPClient,
		Broker:       deps.Broker,
		Stream:       deps.Stream,
		SymbolSearch: deps.SymbolSearch,
	}
	authHandlers := &auth.Handlers{DB: deps.DB, JWTSecret: deps.JWTSecret}
	approvalHandlers := &approvals.Handlers{Service: deps.Approvals, JWTSecret: deps.JWTSecret}

	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", authHandlers.Login)
		v1.GET("/auth/me", auth.MiddlewareAuth(deps.JWTSecret), authHandlers.Me)

		authed := v1.Group("")
		authed.Use(auth.MiddlewareAuth(deps.JWTSecret))
		{
			authed.GET("/overview", api.Overview)
			authed.GET("/portfolio", api.Portfolio)
			authed.GET("/orders", api.ListOrders)
			authed.GET("/stream/market", api.StreamMarket)
			authed.GET("/stream/account", api.StreamAccount)
			authed.GET("/runs", api.ListRuns)
			authed.POST("/runs/trigger", api.PostTriggerRun)
			authed.GET("/runs/:id", api.GetRun)
			authed.POST("/runs/:id/agent-resume", api.PostAgentResume)
			authed.POST("/runs/:id/cancel", approvalHandlers.CancelRun)
			authed.GET("/approvals", api.ListApprovals)
			authed.POST("/approvals/:id/decide", approvalHandlers.Decide)
			authed.GET("/settings", api.Settings)
			authed.POST("/settings/watchlist", api.AddWatchlistSymbol)
			authed.PATCH("/settings/watchlist/:symbol", api.PatchWatchlistSymbol)
			authed.DELETE("/settings/watchlist/:symbol", api.DeleteWatchlistSymbol)
			authed.PATCH("/settings/risk/:key", api.PatchRiskRule)
			authed.GET("/symbols/search", api.SearchSymbols)
			authed.GET("/strategies", api.ListStrategies)
			authed.GET("/strategies/:id", api.GetStrategy)
			authed.POST("/strategies", api.CreateStrategy)
			authed.PATCH("/strategies/:id", api.PatchStrategy)
			authed.POST("/strategies/:id/activate", api.ActivateStrategy)
			authed.DELETE("/strategies/:id", api.DeleteStrategy)
		}
	}

	router.POST("/internal/runs/trigger", api.InternalTriggerRun)

	return router
}
