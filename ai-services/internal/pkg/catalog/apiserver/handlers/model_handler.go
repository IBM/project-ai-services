package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ModelHandler handles model management HTTP requests.
type ModelHandler struct {
	modelService repository.ModelServiceInterface
}

// NewModelHandler creates a new ModelHandler.
func NewModelHandler(modelService repository.ModelServiceInterface) *ModelHandler {
	return &ModelHandler{
		modelService: modelService,
	}
}

// CreateModel godoc
//
//	@Summary		Create a model (local pod or remote connector)
//	@Description	Creates a model deployment. deployment_type=local starts a pod and registers a LiteLLM route asynchronously (202 Accepted). deployment_type=remote registers an external endpoint connector synchronously (201 Created).
//	@Tags			Models
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		models.CreateModelRequest	true	"Model creation request"
//	@Success		201		{object}	models.CreateModelResponse	"Remote model connector created"
//	@Success		202		{object}	models.CreateModelResponse	"Local model deployment initiated"
//	@Failure		400		{object}	ErrorResponse				"Invalid request body or validation errors"
//	@Failure		401		{object}	ErrorResponse				"Unauthorized"
//	@Failure		404		{object}	ErrorResponse				"Provider or worker not found"
//	@Failure		409		{object}	ErrorResponse				"Model or connector name conflict"
//	@Failure		422		{object}	ErrorResponse				"Connectivity check failed"
//	@Failure		500		{object}	ErrorResponse				"Internal Server Error"
//	@Router			/models [post]
func (h *ModelHandler) CreateModel(c *gin.Context) {
	ctx := c.Request.Context()

	var req models.CreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("Invalid request body: %v", err),
		})

		return
	}

	req.CreatedBy = c.GetString(middleware.CtxUserIDKey)

	resp, err := h.modelService.CreateModel(ctx, req)
	if err != nil {
		var valErr *validators.ValidationError
		if errors.As(err, &valErr) {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		// Internal details are logged, not returned: they may include upstream response bodies.
		logger.ErrorfCtx(ctx, "failed to create model: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to create model"})

		return
	}

	c.JSON(createModelStatus(req.DeploymentType), resp)
}

// ListModels godoc
//
//	@Summary		List models (local and remote)
//	@Description	Retrieves a paginated list of the authenticated user's models: local pod-backed models and remote connector-backed models in one list. Only models created by the caller are returned. All filters are combined with AND.
//	@Tags			Models
//	@Produce		json
//	@Security		BearerAuth
//	@Param			component_type	query		string		false	"Filter by model role (e.g. llm, embedding, reranker)"
//	@Param			deployment_type	query		string		false	"Filter by kind: 'local' or 'remote'"
//	@Param			provider_id		query		[]string	false	"Filter by provider (e.g. vllm-spyre, watsonx); repeat the parameter to match any of several"	collectionFormat(multi)
//	@Param			model			query		[]string	false	"Filter by model identifier; repeat the parameter to match any of several"						collectionFormat(multi)
//	@Param			status			query		string		false	"Filter by status: Downloading, Deploying, Running, Error (local) or connected, offline (remote)"
//	@Param			page			query		int			false	"Page number (1-indexed)"				default(1)
//	@Param			page_size		query		int			false	"Number of items per page (max: 100)"	default(20)
//	@Success		200				{object}	models.ModelListResponse
//	@Failure		400				{object}	ErrorResponse	"Invalid query parameters, or a deployment_type, provider_id, model or status that is not valid"
//	@Failure		401				{object}	ErrorResponse	"Unauthorized"
//	@Failure		500				{object}	ErrorResponse	"Internal Server Error"
//	@Router			/models [get]
func (h *ModelHandler) ListModels(c *gin.Context) {
	ctx := c.Request.Context()

	// Reject up front: a list without an owner must never reach the service.
	userID := c.GetString(middleware.CtxUserIDKey)

	page, pageSize, err := parsePagination(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})

		return
	}

	resp, err := h.modelService.ListModels(ctx, models.ListModelsRequest{
		Page:           page,
		PageSize:       pageSize,
		ComponentType:  c.Query("component_type"),
		DeploymentType: constants.DeploymentType(c.Query("deployment_type")),
		ProviderIDs:    queryValues(c, "provider_id"),
		Models:         queryValues(c, "model"),
		Status:         c.Query("status"),
		CreatedBy:      userID,
	})
	if err != nil {
		var valErr *validators.ValidationError
		if errors.As(err, &valErr) {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(ctx, "failed to list models: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list models"})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetModelByID godoc
//
//	@Summary		Get a model by ID (local or remote)
//	@Description	Retrieves one of the authenticated user's models. The id is looked up among local models first, then remote ones. Local and remote models alike are reached through the model gateway, whose public endpoint is returned in endpoints. Secret params of remote models are never returned. Accessing a model created by another user returns 403 Forbidden.
//	@Tags			Models
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Model ID"
//	@Success		200	{object}	models.ModelDetail
//	@Failure		400	{object}	ErrorResponse	"Invalid model ID format"
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		403	{object}	ErrorResponse	"User does not own the model"
//	@Failure		404	{object}	ErrorResponse	"Model not found"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/models/{id} [get]
func (h *ModelHandler) GetModelByID(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid model ID format"})

		return
	}

	resp, err := h.modelService.GetModelByID(ctx, id, c.GetString(middleware.CtxUserIDKey))
	if err != nil {
		var valErr *validators.ValidationError
		if errors.As(err, &valErr) {
			c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

			return
		}

		logger.ErrorfCtx(ctx, "failed to get model %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get model"})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// queryValues returns the values of a repeatable query parameter (?model=a&model=b), without the
// empty ones, so that "?model=" is the same as not filtering.
func queryValues(c *gin.Context, key string) []string {
	var values []string

	for _, v := range c.QueryArray(key) {
		if v != "" {
			values = append(values, v)
		}
	}

	return values
}

// createModelStatus maps a deployment type to its success status: local deployments continue
// in the background (202), remote registrations complete before responding (201).
func createModelStatus(t constants.DeploymentType) int {
	if t == constants.DeploymentTypeLocal {
		return http.StatusAccepted
	}

	return http.StatusCreated
}
