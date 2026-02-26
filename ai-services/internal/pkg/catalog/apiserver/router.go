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
func CreateRouter(authSvc auth.Service, tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Swagger documentation endpoint
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	authHandler := handlers.NewAuthHandler(authSvc)
	v1 := router.Group("/api/v1")
	{
		v1.POST("/auth/login", authHandler.Login)
		v1.POST("/auth/logout", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Logout)
		v1.POST("/auth/refresh", authHandler.Refresh)
		v1.GET("/auth/me", middleware.AuthMiddleware(tokenMgr, blacklist), authHandler.Me)
	}

	applications := v1.Group("applications")
	applications.Use(middleware.AuthMiddleware(tokenMgr, blacklist))

	// Draft endpoints and more discussion needed to finalize the API design. For now, these are placeholders.
	// TODO: Define the API design for application management, including request/response formats and error handling.
	applications.GET("/templates", getTemplates)
	applications.POST("/", createApplication)
	applications.GET("/:name", getApplication)
	applications.DELETE("/:name", deleteApplication)
	applications.GET("/:name/ps", getApplicationStatus)
	applications.POST("/:name/start", startApplication)
	applications.POST("/:name/stop", stopApplication)
	applications.GET("/:name/logs", getApplicationLogs)

	return router
}

// GetTemplates godoc
//
//	@Summary		List application templates
//	@Description	Get a list of available application templates
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	map[string]interface{}	"List of templates"
//	@Router			/applications/templates [get]
func getTemplates(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// CreateApplication godoc
//
//	@Summary		Create new application
//	@Description	Create a new application instance from a template
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	map[string]interface{}	"Application created"
//	@Router			/applications [post]
func createApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// GetApplication godoc
//
//	@Summary		Get application details
//	@Description	Get detailed information about a specific application
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application details"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name} [get]
func getApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// DeleteApplication godoc
//
//	@Summary		Delete application
//	@Description	Delete a specific application and all its resources
//	@Tags			Applications
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application deleted"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name} [delete]
func deleteApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// GetApplicationStatus godoc
//
//	@Summary		Get application status
//	@Description	Get the running status and health of an application
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application status"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name}/ps [get]
func getApplicationStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// StartApplication godoc
//
//	@Summary		Start application
//	@Description	Start a stopped application
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application started"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name}/start [post]
func startApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// StopApplication godoc
//
//	@Summary		Stop application
//	@Description	Stop a running application
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application stopped"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name}/stop [post]
func stopApplication(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}

// GetApplicationLogs godoc
//
//	@Summary		Get application logs
//	@Description	Get logs from a specific application
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			name	path		string					true	"Application name"
//	@Success		200		{object}	map[string]interface{}	"Application logs"
//	@Failure		404		{object}	map[string]interface{}	"Application not found"
//	@Router			/applications/{name}/logs [get]
func getApplicationLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "This is a placeholder endpoint for " + c.FullPath()})
}
