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
		catalog.GET("/architectures", catalogHandler.ListArchitectures)
		catalog.GET("/architectures/:id", catalogHandler.GetArchitectureDetails)
		catalog.GET("/architectures/:id/deploy-options", catalogHandler.GetArchitectureDeployOptions)
		catalog.GET("/services", catalogHandler.ListServices)
		catalog.GET("/services/:id", catalogHandler.GetServiceDetails)
		catalog.GET("/services/:id/deploy-options", catalogHandler.GetServiceDeployOptions)
		catalog.GET("/components/:component_type/providers/:provider_id/params", catalogHandler.GetComponentProviderParams)
	}

	applications := v1.Group("applications")
	applications.Use(middleware.AuthMiddleware(tokenMgr, blacklist))
	{
		// Implemented endpoints
		applications.GET("/", applicationHandler.ListApplications)
		applications.POST("/", applicationHandler.CreateApplication)

		// Draft endpoints - placeholders for future implementation
		applications.GET("/:id", getApplication)
		applications.PUT("/:id", updateApplication)
		applications.DELETE("/:id", deleteApplication)
	}

	return router
}

// GetApplication godoc
//
//	@Summary		Get application details
//	@Description	Get detailed information about a specific application
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Application ID"
//	@Success		200	{object}	map[string]interface{}	"Application details"
//	@Failure		404	{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{id} [get]
func getApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// UpdateApplication godoc
//
//	@Summary		Update application
//	@Description	Update an existing application's configuration
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Application ID"
//	@Success		200	{object}	map[string]interface{}	"Application updated"
//	@Failure		404	{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{id} [put]
func updateApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// DeleteApplication godoc
//
//	@Summary		Delete application
//	@Description	Delete a specific application and all its resources
//	@Tags			Applications
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Application ID"
//	@Success		200	{object}	map[string]interface{}	"Application deleted"
//	@Failure		404	{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{id} [delete]
func deleteApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}
