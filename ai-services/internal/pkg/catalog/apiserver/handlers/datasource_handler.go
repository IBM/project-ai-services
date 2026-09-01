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

// GetDatasource godoc
//
//	@Summary		Get a single datasource connector
//	@Description	Returns the full details of a datasource connector including non-sensitive metadata and the list of connected services enriched with live sync state from each service's Digitize pod.
//	@Tags			Datasources
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string							true	"Datasource UUID"
//	@Success		200	{object}	models.GetDatasourceResponse	"Datasource detail"
//	@Failure		400	{object}	ErrorResponse					"Invalid UUID format"
//	@Failure		401	{object}	ErrorResponse					"Unauthorized"
//	@Failure		404	{object}	ErrorResponse					"Datasource not found"
//	@Failure		500	{object}	ErrorResponse					"Internal Server Error"
//	@Router			/datasources/{id} [get]
func (h *DatasourceHandler) GetDatasource(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid datasource ID format: %v", err),
		})

		return
	}

	resp, err := h.datasourceSvc.GetDatasource(c.Request.Context(), id)
	if err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to get datasource: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to get datasource",
		})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDatasourceServices godoc
//
//	@Summary		Get connected services for a datasource
//	@Description	Returns the list of services currently connected to the given datasource, each enriched with live sync state fetched from its Digitize pod. Sync-state fetch failures degrade gracefully — the service entry is still returned with sync_status set to "unknown".
//	@Tags			Datasources
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string									true	"Datasource UUID"
//	@Success		200	{object}	models.DatasourceServicesResponse		"Connected services with live sync state"
//	@Failure		400	{object}	ErrorResponse							"Invalid UUID format"
//	@Failure		401	{object}	ErrorResponse							"Unauthorized"
//	@Failure		404	{object}	ErrorResponse							"Datasource not found"
//	@Failure		500	{object}	ErrorResponse							"Internal Server Error"
//	@Router			/datasources/{id}/services [get]
func (h *DatasourceHandler) GetDatasourceServices(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid datasource ID format: %v", err),
		})

		return
	}

	resp, err := h.datasourceSvc.GetDatasourceServices(c.Request.Context(), id)
	if err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to get datasource services: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to get datasource services",
		})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteDatasource godoc
//
//	@Summary		Delete datasource connector
//	@Description	Deletes a datasource connector by ID. Returns 409 Conflict if the connector is still linked to one or more applications.
//	@Tags			Datasources
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Datasource connector ID (UUID)"
//	@Success		204	"Datasource deleted"
//	@Failure		400	{object}	ErrorResponse	"Invalid connector ID format"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		404	{object}	ErrorResponse	"Datasource not found"
//	@Failure		409	{object}	ErrorResponse	"Datasource is connected to one or more applications"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/datasources/{id} [delete]
func (h *DatasourceHandler) DeleteDatasource(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid datasource ID format"})

		return
	}

	if err := h.datasourceSvc.DeleteDatasource(c.Request.Context(), id); err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to delete datasource: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})

		return
	}

	c.Status(http.StatusNoContent)
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

// ConnectDatasourcesToApplication godoc
//
//	@Summary		Connect datasources to application
//	@Description	Links one or more datasource connectors to each eligible (Digitize) service in a running application. Each datasource is processed independently. Returns 204 when all succeed; returns 207 with per-datasource errors when one or more fail.
//	@Tags			Datasources
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string								true	"Application ID (UUID)"
//	@Param			request	body	models.ConnectDatasourcesRequest	true	"List of datasource IDs to connect"
//	@Success		204		"All datasources connected successfully"
//	@Success		207		{object}	models.ConnectDatasourcesResponse	"One or more datasources failed to connect"
//	@Failure		400		{object}	ErrorResponse						"Invalid request body"
//	@Failure		401		{object}	ErrorResponse						"Unauthorized"
//	@Failure		404		{object}	ErrorResponse						"Application or datasource not found"
//	@Failure		422		{object}	ErrorResponse						"No eligible running service found"
//	@Failure		500		{object}	ErrorResponse						"Internal Server Error"
//	@Router			/applications/{id}/datasources [put]
func (h *DatasourceHandler) ConnectDatasourcesToApplication(c *gin.Context) {
	applicationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid application ID: %v", err),
		})

		return
	}

	var req models.ConnectDatasourcesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid request body: %v", err),
		})

		return
	}

	// Parse each string UUID into uuid.UUID.
	datasourceIDs := make([]uuid.UUID, 0, len(req.DatasourceIDs))
	for _, idStr := range req.DatasourceIDs {
		id, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: fmt.Sprintf("Invalid datasource ID %q: %v", idStr, parseErr),
			})

			return
		}

		datasourceIDs = append(datasourceIDs, id)
	}

	resp, err := h.datasourceSvc.ConnectDatasourcesToApplication(c.Request.Context(), applicationID, datasourceIDs)
	if err != nil {
		if valErr, ok := err.(*repository.ValidationError); ok {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(c.Request.Context(), "failed to connect datasources to application: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: "Failed to connect datasources to application",
		})

		return
	}

	if resp == nil {
		c.Status(http.StatusNoContent)

		return
	}

	c.JSON(http.StatusMultiStatus, resp)
}

// Made with Bob
