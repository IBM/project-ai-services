package apiserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	_ "github.com/project-ai-services/ai-services/docs" // Import generated docs
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// CreateRouter sets up the Gin router with the necessary routes and authentication middleware for the API server.
func CreateRouter(authSvc auth.Service, tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist, appService *repository.ApplicationService) *gin.Engine {
	router := gin.Default()

	// Apply RequestID middleware to all routes
	router.Use(middleware.RequestIDMiddleware())

	// Health check endpoint
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Expose /health for liveness probes
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Swagger documentation endpoint
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	authHandler := handlers.NewAuthHandler(authSvc)
	catalogHandler := handlers.NewCatalogHandler()
	resourcesHandler := handlers.NewResourcesHandler()
	applicationHandler := handlers.NewApplicationHandler(appService)

	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", authHandler.Login)
		v1.POST("/auth/logout", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Logout)
		v1.POST("/auth/refresh", authHandler.Refresh)
		v1.GET("/auth/me", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Me)
	}

	// Catalog endpoints
	catalog := v1.Group("")
	catalog.Use(middleware.AuthMiddleware(tokenMgr, blacklist))
	{
		catalog.GET("/resources", resourcesHandler.GetResources)
		catalog.GET("/architectures", catalogHandler.ListArchitectures)
		catalog.GET("/architectures/:id", catalogHandler.GetArchitectureDetails)
		catalog.GET("/architectures/:id/deploy-options", catalogHandler.GetArchitectureDeployOptions)
		catalog.GET("/services", catalogHandler.ListServices)
		catalog.GET("/services/:id", catalogHandler.GetServiceDetails)
		catalog.GET("/services/:id/deploy-options", catalogHandler.GetServiceDeployOptions)
		catalog.GET("/services/:id/params", catalogHandler.GetServiceParams)
		catalog.GET("/components/:component_type/providers/:provider_id/params", catalogHandler.GetComponentProviderParams)
	}

	applications := v1.Group("applications")
	applications.Use(middleware.AuthMiddleware(tokenMgr, blacklist))
	{
		applications.GET("/", applicationHandler.ListApplications)
		applications.GET("/:id", applicationHandler.GetApplicationByID)
		applications.GET("/:id/resources", applicationHandler.GetApplicationResources)
		applications.POST("/", applicationHandler.CreateApplication)
		applications.PUT("/:id", applicationHandler.UpdateApplication)
		applications.DELETE("/:id", applicationHandler.DeleteApplication)
		applications.GET("/:id/ps", applicationHandler.ApplicationPS)
	}

	return router
}
