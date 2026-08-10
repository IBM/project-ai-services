// Package connectorhandlers implements the two discovery API handlers for connector
// providers:
//
//	GET /api/v1/connectors?connector_type=<type>            → ListConnectorProviders
//	GET /api/v1/connectors/:connector_type/providers/:provider_id/params → GetConnectorProviderParams
package connectorhandlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/connectors"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// Handler holds the shared ProviderRegistry used by both discovery endpoints.
type Handler struct {
	registry *connectors.ProviderRegistry
}

// New creates a Handler backed by a freshly initialised ProviderRegistry.
// Panics on startup if the registry cannot be loaded (asset files missing /
// malformed), matching the pattern used by NewCatalogHandler.
func New() *Handler {
	registry, err := connectors.NewProviderRegistry()
	if err != nil {
		panic(fmt.Sprintf("failed to initialise connector provider registry: %v", err))
	}

	return &Handler{registry: registry}
}

// ListConnectorProviders godoc
//
//	@Summary		List connector providers
//	@Description	Returns registered providers. When connector_type is supplied only that type is returned; omitting it returns all providers across all connector types.
//	@Tags			Connectors
//	@Produce		json
//	@Security		BearerAuth
//	@Param			connector_type	query		string					false	"Filter by connector type (e.g. 'datasource'). Omit to return all types."
//	@Success		200				{array}		types.ConnectorProvider	"List of providers"
//	@Failure		401				{object}	handlers.ErrorResponse	"Unauthorized"
//	@Failure		404				{object}	handlers.ErrorResponse	"Connector type not found"
//	@Router			/connectors [get]
func (h *Handler) ListConnectorProviders(c *gin.Context) {
	connectorType := c.Query("connector_type")

	var providers []*types.ConnectorProvider
	if connectorType == "" {
		providers = h.registry.ListAllProviders()
	} else {
		var err error
		providers, err = h.registry.ListProviders(connectorType)
		if err != nil {
			c.JSON(http.StatusNotFound, handlers.ErrorResponse{
				Error: fmt.Sprintf("connector type %q not found", connectorType),
			})

			return
		}
	}

	// Return an empty array rather than null when no providers are registered yet.
	if providers == nil {
		providers = []*types.ConnectorProvider{}
	}

	c.JSON(http.StatusOK, providers)
}

// GetConnectorProviderParams godoc
//
//	@Summary		Get connector provider parameters
//	@Description	Returns the JSON Schema for the configuration parameters of a specific connector provider.
//	@Tags			Connectors
//	@Produce		json
//	@Security		BearerAuth
//	@Param			connector_type	path		string					true	"Connector type (e.g. 'datasource')"
//	@Param			provider_id		path		string					true	"Provider identifier (e.g. 's3', 'ssh')"
//	@Success		200				{object}	map[string]interface{}	"JSON Schema for the provider's configuration"
//	@Failure		401				{object}	handlers.ErrorResponse	"Unauthorized"
//	@Failure		404				{object}	handlers.ErrorResponse	"Connector type or provider not found"
//	@Router			/connectors/{connector_type}/providers/{provider_id}/params [get]
func (h *Handler) GetConnectorProviderParams(c *gin.Context) {
	connectorType := c.Param("connector_type")
	providerID := c.Param("provider_id")

	// Verify the provider is registered before serving its schema.
	if _, err := h.registry.GetProviderMetadata(connectorType, providerID); err != nil {
		c.JSON(http.StatusNotFound, handlers.ErrorResponse{
			Error: fmt.Sprintf("provider %q not found for connector type %q", providerID, connectorType),
		})

		return
	}

	schemaPath := filepath.Join("connectors", connectorType, providerID, "schema.json")
	schemaData, err := assets.ConnectorsFS.ReadFile(schemaPath)
	if err != nil {
		c.JSON(http.StatusNotFound, handlers.ErrorResponse{
			Error: fmt.Sprintf("schema not found for provider %q/%q", connectorType, providerID),
		})

		return
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: fmt.Sprintf("failed to parse schema for provider %q/%q", connectorType, providerID),
		})

		return
	}

	c.JSON(http.StatusOK, schema)
}

// Made with Bob
