package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

var (
	ErrInvalidIDParameter = ErrorResponse{Error: "Invalid application ID format"}
)

// Ensure types package is imported for Swagger documentation.
var _ types.ApplicationListResponse
var _ types.ApplicationPSResponse

// ApplicationHandler handles application-related HTTP requests.
type ApplicationHandler struct {
	appService *repository.ApplicationService
}

type UpdateApplicationRequest struct {
	Name string `json:"name" binding:"required,min=3,max=100"`
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
	if deploymentType != "" && deploymentType != string(dbmodels.DeploymentTypeArchitectures) && deploymentType != string(dbmodels.DeploymentTypeServices) {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("deployment_type must be '%s' or '%s'", dbmodels.DeploymentTypeArchitectures, dbmodels.DeploymentTypeServices),
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

// UpdateApplication godoc
//
//	@Summary		Update application
//	@Description	Updates the display name of an existing application
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Application ID (UUID)"
//	@Param			body	body		UpdateApplicationRequest	true	"Update request"
//	@Success		200		{object}	types.Application
//	@Failure		400		{object}	ErrorResponse	"Invalid request body or name validation failed"
//	@Failure		401		{object}	ErrorResponse	"Unauthorized"
//	@Failure		403		{object}	ErrorResponse	"User doesn't own this application"
//	@Failure		404		{object}	ErrorResponse	"Application not found"
//	@Failure		500		{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications/{id} [put]
func (h *ApplicationHandler) UpdateApplication(c *gin.Context) {
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrInvalidIDParameter)

		return
	}
	var req UpdateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("Invalid request body: %v", err)})

		return
	}
	// Get authenticated user ID
	userID := c.GetString(middleware.CtxUserIDKey)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "User not authenticated"})

		return
	}
	updatedApp, err := h.appService.UpdateApplication(c.Request.Context(), appID, userID, req.Name)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("Failed to update application: %v", err)})

		return
	}
	c.JSON(http.StatusOK, updatedApp)
}

// CreateApplication godoc
//
//	@Summary		Create new application
//	@Description	Creates a new application (architecture or service) with optional custom parameters
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		models.CreateApplicationRequest		true	"Application creation request"
//	@Success		202		{object}	models.CreateApplicationResponse	"Application creation initiated"
//	@Failure		400		{object}	ErrorResponse						"Invalid request body or validation errors"
//	@Failure		401		{object}	ErrorResponse						"Unauthorized"
//	@Failure		409		{object}	ErrorResponse						"Application name already exists"
//	@Failure		422		{object}	ErrorResponse						"Parameter validation failed or invalid template"
//	@Failure		500		{object}	ErrorResponse						"Internal Server Error"
//	@Router			/applications [post]
func (h *ApplicationHandler) CreateApplication(c *gin.Context) {
	var req models.CreateApplicationRequest

	// Parse and validate request body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid request body: %v", err),
		})

		return
	}

	// Extract user ID from auth context
	userID := c.GetString(middleware.CtxUserIDKey)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error: "Unauthorized: user ID not found in context",
		})

		return
	}
	req.CreatedBy = userID

	// Call service layer to create application
	response, err := h.appService.CreateApplication(c.Request.Context(), req)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to create application: %v", err),
		})

		return
	}

	c.JSON(http.StatusAccepted, response)
}

// GetApplicationByID godoc
//
//	@Summary		Get application by ID
//	@Description	Retrieves a single application by its unique identifier for the authenticated user
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Application ID"
//	@Success		200	{object}	types.Application
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		404	{object}	ErrorResponse	"Application not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications/{id} [get]
func (h *ApplicationHandler) GetApplicationByID(c *gin.Context) {
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrInvalidIDParameter)

		return
	}

	// Call service layer
	response, err := h.appService.GetApplicationByID(c.Request.Context(), appID)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get application: %v", err),
		})

		return
	}

	c.JSON(http.StatusOK, response)
}

// GetApplicationResources godoc
//
//	@Summary		Get application resources
//	@Description	Retrieves used and total allocated CPU (in cores) and memory usage (in bytes), along with the hardware accelerator cards for an application and its services
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Application ID"
//	@Success		200	{object}	types.ApplicationResourcesResponse
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		404	{object}	ErrorResponse	"Application not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications/{id}/resources [get]
func (h *ApplicationHandler) GetApplicationResources(c *gin.Context) {
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid application ID format"})

		return
	}

	// Call service layer
	response, err := h.appService.GetApplicationResources(c.Request.Context(), appID)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get application resources: %v", err),
		})

		return
	}

	c.JSON(http.StatusOK, response)
}

// DeleteApplication godoc
//
//	@Summary		Delete application
//	@Description	Initiates async deletion of an application and all its resources. Returns 202 immediately.
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string	true	"Application ID (UUID)"
//	@Param			keep_data	query		string	false	"When 'true', preserves underlying data (volumes of databases/service resources). Default: 'false'"
//	@Success		202			{object}	repository.DeleteApplicationResponse
//	@Failure		400			{object}	ErrorResponse	"Invalid application ID or keep_data parameter"
//	@Failure		401			{object}	ErrorResponse	"Unauthorized"
//	@Failure		403			{object}	ErrorResponse	"User doesn't own this application"
//	@Failure		404			{object}	ErrorResponse	"Application not found"
//	@Failure		409			{object}	ErrorResponse	"Application is already being deleted"
//	@Failure		500			{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications/{id} [delete]
func (h *ApplicationHandler) DeleteApplication(c *gin.Context) {
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrInvalidIDParameter)

		return
	}

	userIDVal, exists := c.Get(middleware.CtxUserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "authentication required"})

		return
	}

	userID := userIDVal.(string)

	// Parse and validate keep_data parameter (default: false)
	keepData := false
	keepDataParam := c.Query("keep_data")
	if keepDataParam != "" {
		if keepDataParam != "true" && keepDataParam != "false" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid keep_data parameter: must be 'true' or 'false'"})

			return
		}
		keepData = keepDataParam == "true"
	}

	response, err := h.appService.DeleteApplication(c.Request.Context(), appID, userID, keepData)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})

		return
	}

	c.JSON(http.StatusAccepted, response)
}

// ApplicationPS godoc
//
//	@Summary		Get application process status
//	@Description	Retrieves the process status and runtime information for an application
//	@Tags			Applications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Application ID (UUID)"
//	@Success		200	{object}	types.ApplicationPSResponse
//	@Failure		400	{object}	ErrorResponse	"Invalid application ID format"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		404	{object}	ErrorResponse	"Application not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/applications/{id}/ps [get]
func (h *ApplicationHandler) ApplicationPS(c *gin.Context) {
	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrInvalidIDParameter)

		return
	}

	response, err := h.appService.ApplicationsPs(c.Request.Context(), appID)
	if err != nil {
		// Check if it's a validation error with specific status code
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{
				Error: valErr.Message,
			})

			return
		}

		// Default to internal server error
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get application: %v", err),
		})

		return
	}

	c.JSON(http.StatusOK, response)
}

// Made with Bob
