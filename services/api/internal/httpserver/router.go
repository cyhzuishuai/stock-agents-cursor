package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/approvals"
	"github.com/cyh/stock-agents/services/api/internal/auth"
	"github.com/gin-gonic/gin"
)

func NewRouter(deps RouterDeps) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := &API{
		DB:        deps.DB,
		JWTSecret: deps.JWTSecret,
		Runner:    deps.Runner,
		Approvals: deps.Approvals,
		Ledger:    deps.Ledger,
		Config:    deps.Config,
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
			authed.GET("/runs", api.ListRuns)
			authed.POST("/runs/eod", api.PostEOD)
			authed.GET("/runs/:id", api.GetRun)
			authed.POST("/runs/:id/cancel", approvalHandlers.CancelRun)
			authed.GET("/approvals", api.ListApprovals)
			authed.POST("/approvals/:id/decide", approvalHandlers.Decide)
			authed.GET("/settings", api.Settings)
		}
	}

	router.POST("/internal/eod/run", api.InternalEOD)

	return router
}
