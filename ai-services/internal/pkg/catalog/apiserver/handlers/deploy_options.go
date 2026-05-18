package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
)

// DeployOptionsHandler handles deploy options related HTTP requests.
type DeployOptionsHandler struct {
	provider *catalog.CatalogProvider
}

// NewDeployOptionsHandler creates a new deploy options handler.
func NewDeployOptionsHandler() *DeployOptionsHandler {
	provider, err := catalog.NewCatalogProvider()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize catalog provider: %v", err))
	}

	return &DeployOptionsHandler{
		provider: provider,
	}
}

// GetArchitectureDeployOptions godoc
//
//	@Summary		Get architecture deploy options
//	@Description	Retrieves available providers and dependency rules for all services and their components within an architecture
//	@Tags			Deploy Options
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Architecture ID (e.g., 'rag')"
//	@Success		200	{object}	github_com_project-ai-services_ai-services_internal_pkg_catalog_types.DeployOptionsArchitecture
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Architecture not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/architectures/{id}/deploy-options [get]
func (h *DeployOptionsHandler) GetArchitectureDeployOptions(c *gin.Context) {
	architectureID := c.Param("id")

	deployOptions, err := h.provider.GetArchitectureDeployOptions(architectureID)
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
//	@Tags			Deploy Options
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Service ID (e.g., 'digitize', 'chat')"
//	@Success		200	{object}	github_com_project-ai-services_ai-services_internal_pkg_catalog_types.DeployOptionsService
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		404	{object}	ErrorResponse	"Service not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/services/{id}/deploy-options [get]
func (h *DeployOptionsHandler) GetServiceDeployOptions(c *gin.Context) {
	serviceID := c.Param("id")

	deployOptions, err := h.provider.GetServiceDeployOptions(serviceID)
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
//	@Tags			Deploy Options
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
func (h *DeployOptionsHandler) GetComponentProviderParams(c *gin.Context) {
	componentType := c.Param("component_type")
	providerID := c.Param("provider_id")

	schema, err := h.provider.GetComponentProviderParams(componentType, providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Failed to get parameters for provider '%s/%s': %v", componentType, providerID, err),
		})

		return
	}

	c.JSON(http.StatusOK, schema)
}

// Made with Bob
