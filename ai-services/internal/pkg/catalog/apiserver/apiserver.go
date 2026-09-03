// Package apiserver provides the implementation of the API server for the AI Services Catalog.
// It includes the setup of routes, authentication, and server configuration.
//
//	@title						AI Services Catalog API
//	@version					1.0
//	@description				API server for managing AI Services catalog, applications, and authentication
//	@termsOfService				http://swagger.io/terms/
//
//	@contact.name				API Support
//	@contact.url				https://github.com/project-ai-services/ai-services
//	@contact.email				support@example.com
//
//	@license.name				Apache 2.0
//	@license.url				http://www.apache.org/licenses/LICENSE-2.0.html
//
//	@host						localhost:8080
//	@BasePath					/api/v1
//
//	@tag.name					Authentication
//	@tag.description			Authentication and authorization endpoints
//
//	@tag.name					Applications
//	@tag.description			Application management endpoints
//
//	@tag.name					Catalog
//	@tag.description			Catalog endpoints for architectures and services
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer" followed by a space and JWT token.
package apiserver

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	bundlesvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/gateway"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
)

// APIServerOptions defines the configuration options for the API server such as the port to listen
// on and the authentication provider.
type APIServerOptions struct {
	Port               int
	AuthService        auth.Service
	TokenManager       *auth.TokenManager
	Blacklist          repository.TokenBlacklist
	ApplicationService repository.ApplicationServiceInterface
	DatasourceService  repository.DatasourceServiceInterface
	BundleService      bundlesvc.BundleServiceInterface
	CatalogProvider    *catalog.CatalogProvider

	// WorkerGatewayPort is the port the gRPC worker gateway listens on.
	// Defaults to 9090 when zero.
	WorkerGatewayPort int
	// WorkerRegistry holds the in-memory state of all connected workers and owns
	// the bootstrap token store.
	WorkerRegistry *registry.Registry
	// WorkerRepository is the DB-backed store for worker rows.
	WorkerRepository dbrepo.WorkerRepository
}

// APIserver represents the API server instance, holding the configuration and authentication provider.
type APIserver struct {
	port               int
	authService        auth.Service
	tokenManager       *auth.TokenManager
	blacklist          repository.TokenBlacklist
	applicationService repository.ApplicationServiceInterface
	datasourceService  repository.DatasourceServiceInterface
	bundleService      bundlesvc.BundleServiceInterface
	catalogProvider    *catalog.CatalogProvider

	workerGatewayPort int
	workerRegistry    *registry.Registry
	workerRepository  dbrepo.WorkerRepository
}

// NewAPIserver creates a new instance of the API server with the provided options, setting default values where necessary.
func NewAPIserver(options APIServerOptions) *APIserver {
	// Set default port if not provided
	if options.Port == 0 {
		options.Port = 8080
	}
	if options.WorkerGatewayPort == 0 {
		options.WorkerGatewayPort = 9090
	}

	return &APIserver{
		port:               options.Port,
		authService:        options.AuthService,
		tokenManager:       options.TokenManager,
		blacklist:          options.Blacklist,
		applicationService: options.ApplicationService,
		datasourceService:  options.DatasourceService,
		bundleService:      options.BundleService,
		catalogProvider:    options.CatalogProvider,
		workerGatewayPort:  options.WorkerGatewayPort,
		workerRegistry:     options.WorkerRegistry,
		workerRepository:   options.WorkerRepository,
	}
}

// Start initializes the API server and begins listening for incoming requests on the configured port.
// It sets up the router with authentication middleware and routes, and starts the gRPC worker gateway.
// ctx should be a signal-aware context (e.g. from signal.NotifyContext) so that SIGINT/SIGTERM
// trigger graceful shutdown of the gateway and sweeper.
func (a *APIserver) Start(ctx context.Context) error {
	// Wrap with CancelCause so the gateway can abort the whole process if Serve fails.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	// Start the gRPC worker gateway.
	gw := gateway.New(a.workerRegistry)
	gatewayAddr := fmt.Sprintf(":%d", a.workerGatewayPort)
	if err := gw.Start(ctx, cancel, gatewayAddr); err != nil {
		return fmt.Errorf("failed to start worker gateway: %w", err)
	}
	logger.InfofCtx(ctx, "Worker gateway started on %s", gatewayAddr)

	r := CreateRouter(a.authService, a.tokenManager, a.blacklist, a.applicationService, a.workerRegistry, a.workerRepository, a.datasourceService, a.bundleService, a.catalogProvider)

	if err := r.Run(fmt.Sprintf(":%d", a.port)); err != nil {
		return err
	}

	// If ctx was cancelled by a gateway failure, surface that cause.
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}

	return nil
}
