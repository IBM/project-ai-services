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
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// DatasourceHandler handles datasource connector HTTP requests.
type DatasourceHandler struct {
	datasourceSvc repository.DatasourceServiceInterface
}

// NewDatasourceHandler creates a new DatasourceHandler.
func NewDatasourceHandler(datasourceSvc repository.DatasourceServiceInterface) *DatasourceHandler {
	return &DatasourceHandler{datasourceSvc: datasourceSvc}
}

// CreateDatasource godoc
//
//	@Summary		Create datasource connector
//	@Description	Validates the request, tests the connection, encrypts credentials, and persists a new datasource connector. Returns 422 if the connection test fails.
//	@Tags			Datasources
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		models.CreateDatasourceRequest	true	"Datasource creation request"
//	@Success		201		{object}	models.CreateDatasourceResponse	"Datasource created"
//	@Failure		400		{object}	ErrorResponse					"Invalid request body or validation errors"
//	@Failure		401		{object}	ErrorResponse					"Unauthorized"
//	@Failure		404		{object}	ErrorResponse					"Provider not found in catalog"
//	@Failure		409		{object}	ErrorResponse					"Datasource name already exists"
//	@Failure		422		{object}	ErrorResponse					"Connection test failed"
//	@Failure		500		{object}	ErrorResponse					"Internal Server Error"
//	@Router			/datasources [post]
func (h *DatasourceHandler) CreateDatasource(c *gin.Context) {
	var req models.CreateDatasourceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid request body: %v", err),
		})

		return
	}

	// Extract authenticated user from context.
	userID := c.GetString(middleware.CtxUserIDKey)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error: "Unauthorized: user ID not found in context",
		})

		return
	}

	req.CreatedBy = userID

	resp, err := h.datasourceSvc.CreateDatasource(c.Request.Context(), req)
	if err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to create datasource: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to create datasource: %v", err),
		})

		return
	}

	c.JSON(http.StatusCreated, resp)
}

// ListDatasources godoc
//
//	@Summary		List datasource connectors
//	@Description	Returns a paginated list of datasource connectors with optional filters by status and provider.
//	@Tags			Datasources
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int		false	"Page number (1-indexed)"				default(1)
//	@Param			page_size	query		int		false	"Number of items per page (max: 100)"	default(20)
//	@Param			status		query		string	false	"Filter by status: 'connected' or 'offline'"
//	@Param			provider	query		string	false	"Filter by provider ID (e.g. 'object_storage', 'file_system')"
//	@Success		200			{object}	models.DatasourceListResponse
//	@Failure		400			{object}	ErrorResponse	"Invalid query parameters"
//	@Failure		401			{object}	ErrorResponse	"Unauthorized"
//	@Failure		500			{object}	ErrorResponse	"Internal Server Error"
//	@Router			/datasources [get]
func (h *DatasourceHandler) ListDatasources(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	page, pageSize, err := repository.ValidatePaginationParams(page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})

		return
	}

	status := c.Query("status")
	if status != "" &&
		dbmodels.ConnectorStatus(status) != dbmodels.ConnectorStatusConnected &&
		dbmodels.ConnectorStatus(status) != dbmodels.ConnectorStatusOffline {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("status must be '%s' or '%s'", dbmodels.ConnectorStatusConnected, dbmodels.ConnectorStatusOffline),
		})

		return
	}

	req := models.ListDatasourcesRequest{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
		Provider: c.Query("provider"),
	}

	resp, err := h.datasourceSvc.ListDatasources(c.Request.Context(), req)
	if err != nil {
		logger.ErrorfCtx(c.Request.Context(), "failed to list datasources: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to list datasources",
		})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateDatasource godoc
//
//	@Summary		Update datasource credentials
//	@Description	Updates the Authentication credential fields for a datasource as defined in the provider schema. Re-runs the connectivity test before saving. If any linked Digitize services cannot be notified, the datasource is still updated and the failures are listed in propagation_errors.
//	@Tags			Datasources
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Datasource ID (UUID)"
//	@Param			request	body		models.UpdateDatasourceRequest	true	"Credential update request"
//	@Success		200		{object}	models.UpdateDatasourceResponse	"Datasource updated"
//	@Failure		400		{object}	ErrorResponse					"Invalid request body or invalid ID"
//	@Failure		401		{object}	ErrorResponse					"Unauthorized"
//	@Failure		404		{object}	ErrorResponse					"Datasource not found"
//	@Failure		422		{object}	ErrorResponse					"Connection test failed with new credentials"
//	@Failure		500		{object}	ErrorResponse					"Internal Server Error"
//	@Router			/datasources/{id} [put]
func (h *DatasourceHandler) UpdateDatasource(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid datasource ID format"})

		return
	}

	var req models.UpdateDatasourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("invalid request body: %v", err),
		})

		return
	}

	resp, err := h.datasourceSvc.UpdateDatasource(c.Request.Context(), id, req)
	if err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to update datasource %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "failed to update datasource",
		})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// Made with Bob
