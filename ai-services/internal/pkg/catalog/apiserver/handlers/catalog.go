package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// CatalogHandler handles catalog-related HTTP requests.
type CatalogHandler struct{}

// NewCatalogHandler creates a new catalog handler.
func NewCatalogHandler() *CatalogHandler {
	return &CatalogHandler{}
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
	architectures, err := catalog.ListArchitectures()
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

	architecture, err := catalog.LoadArchitecture(id)
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
//	@Description	Retrieves a list of all deployable service templates. Dependency-only services are excluded from this list. Returns service summaries without endpoints and pod templates.
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

	servicesList, err := catalog.ListServicesWithRuntime(runtime)
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

	service, err := catalog.LoadService(id)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error: fmt.Sprintf("Service '%s' not found: %v", id, err),
		})

		return
	}

	c.JSON(http.StatusOK, service)
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Made with Bob
