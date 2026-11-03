package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
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
//	@Param			request	body		models.CreateModelRequest		true	"Model creation request"
//	@Success		201		{object}	models.CreateModelResponse		"Remote model connector created"
//	@Success		202		{object}	models.CreateModelResponse		"Local model deployment initiated"
//	@Failure		400		{object}	ErrorResponse					"Invalid request body or validation errors"
//	@Failure		401		{object}	ErrorResponse					"Unauthorized"
//	@Failure		404		{object}	ErrorResponse					"Provider or worker not found"
//	@Failure		409		{object}	ErrorResponse					"Model or connector name conflict"
//	@Failure		422		{object}	ErrorResponse					"Connectivity check failed"
//	@Failure		500		{object}	ErrorResponse					"Internal Server Error"
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

	c.JSON(createModelStatus(resp.DeploymentType), resp)
}

// createModelStatus maps a deployment type to its success status: local deployments continue
// in the background (202), remote registrations complete before responding (201).
func createModelStatus(t models.DeploymentType) int {
	if t == models.DeploymentTypeLocal {
		return http.StatusAccepted
	}

	return http.StatusCreated
}
