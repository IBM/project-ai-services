package apiserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
)

// CreateRouter sets up the Gin router with the necessary routes and authentication middleware for the API server.
func CreateRouter(authSvc auth.Service, tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist) *gin.Engine {
	router := gin.Default()
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	authHandler := handlers.NewAuthHandler(authSvc)
	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", authHandler.Login)
		v1.POST("/auth/logout", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Logout)
		v1.POST("/auth/refresh", authHandler.Refresh)
		v1.GET("/auth/me", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Me)
	}

	applications := v1.Group("applications")

	// Draft endpoints and more discussion needed to finalize the API design. For now, these are placeholders.
	// TODO: Define the API design for application management, including request/response formats and error handling.
	applications.GET("/templates", dummy)
	applications.POST("/", dummy)
	applications.GET("/:name", dummy)
	applications.DELETE("/:name", dummy)
	applications.GET("/:name/ps", dummy)
	applications.POST("/:name/start", dummy)
	applications.POST("/:name/stop", dummy)
	applications.GET("/:name/logs", dummy)

	return router
}

func dummy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}
