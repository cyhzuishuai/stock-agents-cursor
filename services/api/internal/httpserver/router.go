package httpserver

import (
	"net/http"

	"github.com/cyh/stock-agents/services/api/internal/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB, jwtSecret string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	handlers := &auth.Handlers{DB: db, JWTSecret: jwtSecret}
	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", handlers.Login)
		v1.GET("/auth/me", auth.MiddlewareAuth(jwtSecret), handlers.Me)
	}

	return router
}
