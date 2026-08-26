package apiserver

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/project-ai-services/ai-services/docs" // Import generated docs
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	bundlesvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// CreateRouter sets up the Gin router with the necessary routes and authentication middleware for the API server.
func CreateRouter(authSvc auth.Service, tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist, appService repository.ApplicationServiceInterface, workerReg *registry.Registry, datasourceSvc repository.DatasourceServiceInterface, bundleService bundlesvc.BundleServiceInterface, catalogProvider *catalog.CatalogProvider) *gin.Engine {
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	}
	router := gin.Default()

	// Apply RequestID middleware to all routes
	router.Use(middleware.RequestIDMiddleware())
	// Health check endpoint
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"message": "ok"}) })
	// Expose /health for liveness probes
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"message": "ok"}) })
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := router.Group("/api/v1")
	registerAuthRoutes(v1, handlers.NewAuthHandler(authSvc), tokenMgr, blacklist)

	auth := middleware.AuthMiddleware(tokenMgr, blacklist)
	datasourceH := handlers.NewDatasourceHandler(datasourceSvc)
	registerCatalogRoutes(v1, handlers.NewCatalogHandler(catalogProvider), handlers.NewResourcesHandler(), auth)
	registerApplicationRoutes(v1, handlers.NewApplicationHandler(appService), datasourceH, auth)
	registerWorkerRoutes(v1, handlers.NewWorkerHandler(workerReg), auth)
	registerDatasourceRoutes(v1, datasourceH, auth)
	registerBundleRoutes(v1, handlers.NewBundleHandler(bundleService), auth)

	return router
}

func registerAuthRoutes(v1 *gin.RouterGroup, h *handlers.AuthHandler, tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist) {
	authMw := middleware.AuthMiddleware(tokenMgr, blacklist)
	v1.POST("/auth/login", h.Login)
	v1.POST("/auth/token", h.TokenLogin)
	v1.POST("/auth/logout", authMw, h.Logout)
	v1.POST("/auth/refresh", h.Refresh)
	v1.GET("/auth/me", authMw, h.Me)
}

func registerCatalogRoutes(v1 *gin.RouterGroup, catalog *handlers.CatalogHandler, resources *handlers.ResourcesHandler, authMw gin.HandlerFunc) {
	g := v1.Group("")
	g.Use(authMw)
	{
		g.GET("/resources", resources.GetResources)
		g.GET("/architectures", catalog.ListArchitectures)
		g.GET("/architectures/:id", catalog.GetArchitectureDetails)
		g.GET("/architectures/:id/deploy-options", catalog.GetArchitectureDeployOptions)
		g.GET("/services", catalog.ListServices)
		g.GET("/services/:id", catalog.GetServiceDetails)
		g.GET("/services/:id/deploy-options", catalog.GetServiceDeployOptions)
		g.GET("/services/:id/params", catalog.GetServiceParams)
		g.GET("/components/:component_type/providers/:provider_id/params", catalog.GetComponentProviderParams)
		g.GET("/connectors", catalog.ListConnectorProviders)
		g.GET("/connectors/:connector_type/providers/:provider_id/params", catalog.GetConnectorProviderParams)
	}
}

func registerBundleRoutes(v1 *gin.RouterGroup, h *handlers.BundleHandler, authMw gin.HandlerFunc) {
	g := v1.Group("catalog/bundles")
	g.Use(authMw)
	{
		// POST /api/v1/catalog/bundles — create a new bundle
		g.POST("", h.CreateBundle)
		// POST /api/v1/catalog/bundles/validate — validate without storing
		g.POST("/validate", h.ValidateBundle)
		// GET /api/v1/catalog/bundles — list all bundles
		g.GET("", h.ListBundles)
		// GET /api/v1/catalog/bundles/:id — get a single bundle
		g.GET("/:id", h.GetBundle)
		// PUT /api/v1/catalog/bundles/:id — replace an existing bundle
		g.PUT("/:id", h.UpdateBundle)
		// DELETE /api/v1/catalog/bundles/:id — delete a bundle
		g.DELETE("/:id", h.DeleteBundle)
	}
}

func registerApplicationRoutes(v1 *gin.RouterGroup, h *handlers.ApplicationHandler, datasourceH *handlers.DatasourceHandler, authMw gin.HandlerFunc) {
	g := v1.Group("applications")
	g.Use(authMw)
	{
		g.GET("/", h.ListApplications)
		g.GET("/:id", h.GetApplicationByID)
		g.GET("/:id/resources", h.GetApplicationResources)
		g.POST("/", h.CreateApplication)
		g.PUT("/:id", h.UpdateApplication)
		g.DELETE("/:id", h.DeleteApplication)
		g.GET("/:id/ps", h.ApplicationPS)
		// PUT /api/v1/applications/:id/datasources/:datasource_id — connect datasource to application
		g.PUT("/:id/datasources/:datasource_id", datasourceH.ConnectDatasourceToApplication)
	}
}

func registerWorkerRoutes(v1 *gin.RouterGroup, h *handlers.WorkerHandler, authMw gin.HandlerFunc) {
	g := v1.Group("workers")
	g.Use(authMw)
	{
		g.POST("", h.CreateWorker)
		g.GET("", h.ListWorkers)
		g.DELETE("/:id", h.DeleteWorker)
	}
}

func registerDatasourceRoutes(v1 *gin.RouterGroup, h *handlers.DatasourceHandler, authMw gin.HandlerFunc) {
	g := v1.Group("datasources")
	g.Use(authMw)
	{
		g.POST("", h.CreateDatasource)
	}
}
