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
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer" followed by a space and JWT token.
package apiserver

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
)

// APIServerOptions defines the configuration options for the API server such as the port to listen
// on and the authentication provider.
type APIServerOptions struct {
	Port         int
	AuthService  auth.Service
	TokenManager *auth.TokenManager
	Blacklist    repository.TokenBlacklist
}

// APIserver represents the API server instance, holding the configuration and authentication provider.
type APIserver struct {
	port         int
	authService  auth.Service
	tokenManager *auth.TokenManager
	blacklist    repository.TokenBlacklist
}

// NewAPIserver creates a new instance of the API server with the provided options, setting default values where necessary.
func NewAPIserver(options APIServerOptions) *APIserver {
	// Set default port if not provided
	if options.Port == 0 {
		options.Port = 8080
	}

	return &APIserver{
		port:         options.Port,
		authService:  options.AuthService,
		tokenManager: options.TokenManager,
		blacklist:    options.Blacklist,
	}
}

// Start initializes the API server and begins listening for incoming requests on the configured port.
// It sets up the router with authentication middleware and routes.
func (a *APIserver) Start() error {
	r := CreateRouter(a.authService, a.tokenManager, a.blacklist)

	return r.Run(fmt.Sprintf(":%d", a.port))
}
