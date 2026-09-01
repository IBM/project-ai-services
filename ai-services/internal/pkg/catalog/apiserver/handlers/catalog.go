package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// CatalogHandler handles catalog-related HTTP requests.
type CatalogHandler struct {
	provider *catalog.CatalogProvider
}

// NewCatalogHandler creates a new catalog handler backed by the given provider.
func NewCatalogHandler(provider *catalog.CatalogProvider) *CatalogHandler {
	return &CatalogHandler{provider: provider}
}

// ListArchitectures godoc
//
//	@Summary		List available architectures
//	@Description	Retrieves a list of all available architecture templates with summary information
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		types.ArchitectureSummary
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures [get]
func (h *CatalogHandler) ListArchitectures(c *gin.Context) {
	architectures, err := h.provider.ListArchitectures()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to list architectures: %v", err),
		})

		return
	}

	// Convert to summaries
	summaries := make([]types.ArchitectureSummary, len(architectures))
	for i, arch := range architectures {
		summaries[i] = catalog.ToArchitectureSummary(&arch)
	}

	c.JSON(http.StatusOK, summaries)
}

// GetArchitectureDetails godoc
//
//	@Summary		Get architecture details
//	@Description	Retrieves detailed information about a specific architecture template
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Architecture template ID (e.g., 'rag')"
//	@Success		200	{object}	types.Architecture
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Architecture not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures/{id} [get]
func (h *CatalogHandler) GetArchitectureDetails(c *gin.Context) {
	id := c.Param("id")

	architecture, err := h.provider.LoadArchitecture(id)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Architecture '%s' not found: %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, architecture)
}

// ListServices godoc
//
//	@Summary		List available services
//	@Description	Retrieves a list of all deployable service templates. Dependency-only services are excluded from this list. Returns service summaries including standalone flag without endpoints and pod templates.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		types.ServiceSummary
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services [get]
func (h *CatalogHandler) ListServices(c *gin.Context) {
	// Get runtime from global factory
	runtime := vars.RuntimeFactory.GetRuntimeType()

	servicesList, err := h.provider.ListServicesWithRuntime(runtime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to list services: %v", err),
		})

		return
	}

	// Convert to summaries (exclude endpoints and pod_templates)
	summaries := make([]types.ServiceSummary, len(servicesList))
	for i, svc := range servicesList {
		summaries[i] = catalog.ToServiceSummary(&svc)
	}

	c.JSON(http.StatusOK, summaries)
}

// GetServiceDetails godoc
//
//	@Summary		Get service details
//	@Description	Retrieves detailed information about a specific service template
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Service template ID (e.g., 'summarize')"
//	@Success		200	{object}	types.Service
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Service not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services/{id} [get]
func (h *CatalogHandler) GetServiceDetails(c *gin.Context) {
	id := c.Param("id")

	service, err := h.provider.LoadService(id)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Service '%s' not found: %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, service)
}

// GetArchitectureDeployOptions godoc
//
//	@Summary		Get architecture deploy options
//	@Description	Retrieves available providers and dependency rules for all services and their components within an architecture
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Architecture ID (e.g., 'rag')"
//	@Success		200	{object}	types.DeployOptionsArchitecture
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Architecture not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures/{id}/deploy-options [get]
func (h *CatalogHandler) GetArchitectureDeployOptions(c *gin.Context) {
	architectureID := c.Param("id")

	deployOptions, err := h.provider.GetArchitectureDeployOptions(c.Request.Context(), architectureID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get deploy options for architecture '%s': %v", architectureID, err),
		})

		return
	}

	c.JSON(http.StatusOK, deployOptions)
}

// GetServiceDeployOptions godoc
//
//	@Summary		Get service deploy options
//	@Description	Retrieves available providers and dependency rules for a specific service
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Service ID (e.g., 'digitize', 'chat')"
//	@Success		200	{object}	types.DeployOptionsService
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Service not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services/{id}/deploy-options [get]
func (h *CatalogHandler) GetServiceDeployOptions(c *gin.Context) {
	serviceID := c.Param("id")

	deployOptions, err := h.provider.GetServiceDeployOptions(c.Request.Context(), serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get deploy options for service '%s': %v", serviceID, err),
		})

		return
	}

	c.JSON(http.StatusOK, deployOptions)
}

// GetComponentProviderParams godoc
//
//	@Summary		Get component provider parameters
//	@Description	Retrieves the configuration schema (JSON Schema) for a specific provider within a component type. Returns a JSON Schema object with properties that may include x-data-id for fields that should be populated from metadata specifications.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			component_type	path		string					true	"Component type (e.g., 'vector_db', 'llm', 'embedding', 'reranker')"
//	@Param			provider_id		path		string					true	"Provider identifier (e.g., 'opensearch', 'vllm', 'watsonx')"
//	@Success		200				{object}	map[string]interface{}	"JSON Schema object with $schema, type, and properties. Properties may include x-data-id field indicating data should be populated from metadata specifications (e.g., supported_models)"
//	@Failure		400				{object}	ErrorResponse			"Bad Request - Invalid component_type or provider_id"
//	@Failure		401				{object}	ErrorResponse			"Unauthorized - Invalid or missing access token"
//	@Failure		404				{object}	ErrorResponse			"Component type or provider not found"
//	@Failure		500				{object}	ErrorResponse			"Internal Server Error"
//	@Router			/components/{component_type}/providers/{provider_id}/params [get]
func (h *CatalogHandler) GetComponentProviderParams(c *gin.Context) {
	componentType := c.Param("component_type")
	providerID := c.Param("provider_id")

	schema, err := h.provider.GetComponentProviderParams(c.Request.Context(), componentType, providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get parameters for provider '%s/%s': %v", componentType, providerID, err),
		})

		return
	}

	c.JSON(http.StatusOK, schema)
}

// ListConnectorProviders godoc
//
//	@Summary		List connector providers
//	@Description	Returns registered providers. When type is supplied only that type is returned; omitting it returns all providers across all connector types.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			type	query		string					false	"Filter by connector type (e.g. 'datasource'). Omit to return all types."
//	@Success		200		{array}		types.ConnectorResponse	"List of providers"
//	@Failure		401		{object}	ErrorResponse			"Unauthorized"
//	@Failure		404		{object}	ErrorResponse			"Connector type not found"
//	@Router			/connectors [get]
func (h *CatalogHandler) ListConnectorProviders(c *gin.Context) {
	connectorType := c.Query("type")

	var connectors []*types.Connector
	if connectorType == "" {
		connectors = h.provider.ListAllConnectors()
	} else {
		var err error
		connectors, err = h.provider.ListConnectors(connectorType)
		if err != nil {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error: fmt.Sprintf("connector type %q not found", connectorType),
			})

			return
		}
	}

	response := make([]types.ConnectorResponse, len(connectors))
	for i, conn := range connectors {
		response[i] = types.ToConnectorResponse(conn)
	}

	c.JSON(http.StatusOK, response)
}

// GetConnectorProviderParams godoc
//
//	@Summary		Get connector provider parameters
//	@Description	Returns the JSON Schema for the configuration parameters of a specific connector provider.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			connector_type	path		string					true	"Connector type (e.g. 'datasource')"
//	@Param			provider_id		path		string					true	"Provider identifier (e.g. 'object_storage', 'file_system')"
//	@Success		200				{object}	map[string]interface{}	"JSON Schema for the provider's configuration"
//	@Failure		401				{object}	ErrorResponse			"Unauthorized"
//	@Failure		404				{object}	ErrorResponse			"Connector type or provider not found"
//	@Router			/connectors/{connector_type}/providers/{provider_id}/params [get]
func (h *CatalogHandler) GetConnectorProviderParams(c *gin.Context) {
	connectorType := c.Param("connector_type")
	providerID := c.Param("provider_id")

	raw, err := h.provider.GetConnectorProviderParams(c.Request.Context(), connectorType, providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get parameters for provider '%s/%s': %v", connectorType, providerID, err),
		})

		return
	}

	// Write the raw JSON bytes directly so the property order defined in schema.json is preserved.
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
}

// GetServiceParams godoc
//
//	@Summary		Get service parameters
//	@Description	Retrieves the configuration schema (JSON Schema) for a specific service. Returns a JSON Schema object with properties that define the service's configurable parameters.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Service ID (e.g., 'chat', 'digitize', 'similarity')"
//	@Success		200	{object}	map[string]interface{}	"JSON Schema object with $schema, type, and properties defining service parameters"
//	@Failure		400	{object}	ErrorResponse			"Bad Request - Invalid service ID"
//	@Failure		401	{object}	ErrorResponse			"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse			"Service not found"
//	@Failure		500	{object}	ErrorResponse			"Internal Server Error"
//	@Router			/services/{id}/params [get]
func (h *CatalogHandler) GetServiceParams(c *gin.Context) {
	serviceID := c.Param("id")

	schema, err := h.provider.GetServiceParams(c.Request.Context(), serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get parameters for service '%s': %v", serviceID, err),
		})

		return
	}

	c.JSON(http.StatusOK, schema)
}

// GetServiceImages godoc
//
//	@Summary		Get service images
//	@Description	Returns the complete list of container images required to deploy a service and all its component dependencies.
//	@Description	The response always includes catalog asset images (the tool image used for housekeeping tasks
//	@Description	and the catalog infrastructure images for the catalog service itself).
//	@Description	Both embedded (built-in) and custom bundle services are supported.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string			true	"Service ID (e.g., 'chat', 'digitize', 'summarize')"
//	@Success		200	{array}		string			"List of container image references"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Service not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services/{id}/images [get]
func (h *CatalogHandler) GetServiceImages(c *gin.Context) {
	id := c.Param("id")

	images, err := h.provider.GetServiceImages(c.Request.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrCatalogItemNotFound) {
			status = http.StatusNotFound
		}

		c.JSON(status, ErrorResponse{
			Error: fmt.Sprintf("Failed to get images for service '%s': %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, images)
}

// GetArchitectureImages godoc
//
//	@Summary		Get architecture images
//	@Description	Returns the complete list of container images required to deploy an architecture and all its services and component dependencies.
//	@Description	The response always includes catalog asset images (the tool image used for housekeeping tasks
//	@Description	and the catalog infrastructure images for the catalog service itself).
//	@Description	Both embedded (built-in) and custom bundle architectures are supported.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string			true	"Architecture ID (e.g., 'rag')"
//	@Success		200	{array}		string			"List of container image references"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Architecture not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures/{id}/images [get]
func (h *CatalogHandler) GetArchitectureImages(c *gin.Context) {
	id := c.Param("id")

	images, err := h.provider.GetArchitectureImages(c.Request.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrCatalogItemNotFound) {
			status = http.StatusNotFound
		}

		c.JSON(status, ErrorResponse{
			Error: fmt.Sprintf("Failed to get images for architecture '%s': %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, images)
}

// GetServiceModels godoc
//
//	@Summary		Get models for a service
//	@Description	Returns all unique model names referenced in the schema of a service's component dependencies
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string			true	"Service ID (e.g., 'chat', 'digitize')"
//	@Success		200	{array}		string			"List of unique model names"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Service not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services/{id}/models [get]
func (h *CatalogHandler) GetServiceModels(c *gin.Context) {
	id := c.Param("id")
	excludeProviders := parseExcludeProviders(c)

	models, err := h.provider.GetServiceModels(c.Request.Context(), id, excludeProviders...)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrCatalogItemNotFound) {
			status = http.StatusNotFound
		}

		c.JSON(status, ErrorResponse{
			Error: fmt.Sprintf("Failed to get models for service '%s': %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, models)
}

// GetArchitectureModels godoc
//
//	@Summary		Get models for an architecture
//	@Description	Returns all unique model names referenced in the schemas of all component dependencies across every service in the architecture
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string			true	"Architecture ID (e.g., 'rag')"
//	@Success		200	{array}		string			"List of unique model names"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Architecture not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures/{id}/models [get]
func (h *CatalogHandler) GetArchitectureModels(c *gin.Context) {
	id := c.Param("id")
	excludeProviders := parseExcludeProviders(c)

	models, err := h.provider.GetArchitectureModels(c.Request.Context(), id, excludeProviders...)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrCatalogItemNotFound) {
			status = http.StatusNotFound
		}

		c.JSON(status, ErrorResponse{
			Error: fmt.Sprintf("Failed to get models for architecture '%s': %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, models)
}

// parseExcludeProviders reads the comma-separated ?exclude_providers= query
// parameter and returns a slice of provider IDs to exclude from model results.
func parseExcludeProviders(c *gin.Context) []string {
	raw := c.Query("exclude_providers")
	if raw == "" {
		return nil
	}

	var result []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}

	return result
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Made with Bob
