package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

type ApplicationHandler struct {
	runtimeType types.RuntimeType
}

func NewApplicationHandler(runtimeType types.RuntimeType) *ApplicationHandler {
	return &ApplicationHandler{runtimeType: runtimeType}
}

type createApplicationReq struct {
	Name              string            `json:"name" binding:"required"`
	TemplateName      string            `json:"template_name" binding:"required"`
	ArgParams         map[string]string `json:"arg_params,omitempty"`
	ValuesFiles       []string          `json:"values_files,omitempty"`
	SkipModelDownload bool              `json:"skip_model_download,omitempty"`
	SkipImageDownload bool              `json:"skip_image_download,omitempty"`
	ImagePullPolicy   string            `json:"image_pull_policy,omitempty"`
}

// CreateApplication godoc
//
//	@Summary		Create new application
//	@Description	Create a new application instance from a template
//	@Tags			Applications
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			application	body		createApplicationReq	true	"Application creation request"
//	@Success		201			{object}	map[string]interface{}	"Application created successfully"
//	@Failure		400			{object}	map[string]interface{}	"Invalid request payload"
//	@Failure		500			{object}	map[string]interface{}	"Failed to create application"
//	@Router			/applications [post]
func (h *ApplicationHandler) CreateApplication(c *gin.Context) {
	var req createApplicationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload", "details": err.Error()})
		return
	}

	ctx := context.Background()

	// Set default image pull policy if not provided
	pullPolicy := image.PullIfNotPresent
	if req.ImagePullPolicy != "" {
		pullPolicy = image.ImagePullPolicy(req.ImagePullPolicy)
		if !pullPolicy.Valid() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image_pull_policy", "details": "must be one of: Always, Never, IfNotPresent"})
			return
		}
	}

	// Create application factory and instance
	appFactory := application.NewFactory(h.runtimeType)
	app, err := appFactory.Create(req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create application instance", "details": err.Error()})
		return
	}

	// Build CreateOptions
	opts := appTypes.CreateOptions{
		Name:              req.Name,
		TemplateName:      req.TemplateName,
		ArgParams:         req.ArgParams,
		ValuesFiles:       req.ValuesFiles,
		SkipModelDownload: req.SkipModelDownload,
		ImagePullPolicy:   pullPolicy,
	}

	// Call the Create method from internal/pkg/application
	if err := app.Create(ctx, opts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create application", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "application created successfully",
		"name":     req.Name,
		"template": req.TemplateName,
	})
}

// Made with Bob
