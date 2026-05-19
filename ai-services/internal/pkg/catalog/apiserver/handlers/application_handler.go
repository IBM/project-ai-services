package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// Ensure types package is imported for Swagger documentation.
var _ types.ApplicationListResponse

// ApplicationHandler handles application-related HTTP requests.
type ApplicationHandler struct {
	appService *repository.ApplicationService
}

// NewApplicationHandler creates a new application handler.
func NewApplicationHandler(appService *repository.ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{
		appService: appService,
	}
}

// ListApplications godoc
//
//	@Summary		List applications
//	@Description	Retrieves a paginated list of all applications for the authenticated user with optional filters
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page			query		int		false	"Page number (1-indexed)"				default(1)
//	@Param			page_size		query		int		false	"Number of items per page (max: 100)"	default(20)
//	@Param			deployment_type	query		string	false	"Filter by deployment type: 'architectures' or 'services'"
//	@Param			catalog_id		query		string	false	"Filter by catalog ID (e.g., 'rag', 'chat', 'digitize', 'summarize')"
//	@Success		200				{object}	types.ApplicationListResponse
//	@Failure		400				{object}	ErrorResponse	"Invalid query parameters"
//	@Failure		401				{object}	ErrorResponse	"Unauthorized"
//	@Failure		500				{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications [get]
func (h *ApplicationHandler) ListApplications(c *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	// Validate and apply defaults
	page, pageSize, err := repository.ValidatePaginationParams(page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})

		return
	}

	// Parse filter parameters
	deploymentType := c.Query("deployment_type")
	catalogID := c.Query("catalog_id")

	// Validate deployment_type if provided
	if deploymentType != "" && deploymentType != string(models.DeploymentTypeArchitectures) && deploymentType != string(models.DeploymentTypeServices) {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("deployment_type must be '%s' or '%s'", models.DeploymentTypeArchitectures, models.DeploymentTypeServices),
		})

		return
	}

	// Build request
	req := repository.ListApplicationsRequest{
		Page:           page,
		PageSize:       pageSize,
		DeploymentType: deploymentType,
		CatalogID:      catalogID,
	}

	// Call service layer
	response, err := h.appService.ListApplications(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to retrieve applications: %v", err),
		})

		return
	}

	c.JSON(http.StatusOK, response)
}

// Made with Bob
