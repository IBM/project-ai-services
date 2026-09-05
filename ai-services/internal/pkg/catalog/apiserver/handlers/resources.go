package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// ResourcesHandler handles resources-related HTTP requests.
type ResourcesHandler struct {
	workerRegistry stream.WorkerRegistry
}

// NewResourcesHandler creates a new resources handler.
// workerRegistry may be nil when worker support is not configured.
func NewResourcesHandler(workerRegistry stream.WorkerRegistry) *ResourcesHandler {
	return &ResourcesHandler{workerRegistry: workerRegistry}
}

// ResourcesResponse represents system resource information.
type ResourcesResponse struct {
	CPU          *models.CPUInfo                    `json:"cpu,omitempty"`
	Memory       *models.MemoryInfo                 `json:"memory,omitempty"`
	Accelerators map[string]*models.AcceleratorInfo `json:"accelerators"`
}

// GetResources godoc
//
//	@Summary		Get system resources
//	@Description	Retrieves system resource information including CPU, memory, and accelerator availability.
//	@Description	When the optional `worker` query parameter is provided, the resources are fetched from
//	@Description	that remote worker node instead of the local runtime.
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			worker	query		string	false	"Worker name to query resources from"
//	@Success		200		{object}	ResourcesResponse
//	@Failure		400		{object}	ErrorResponse	"Worker not connected"
//	@Failure		401		{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		500		{object}	ErrorResponse	"Internal Server Error"
//	@Router			/resources [get]
func (h *ResourcesHandler) GetResources(c *gin.Context) {
	ctx := c.Request.Context()

	workerName := c.Query("worker")

	var rt runtime.Runtime

	if workerName != "" && h.workerRegistry != nil {
		rtStr, ok := h.workerRegistry.WorkerRuntimeType(workerName)
		if !ok {
			c.JSON(http.StatusBadRequest, ErrorResponse{
				Error: fmt.Sprintf("worker %q is not connected", workerName),
			})

			return
		}

		rt = remoteRuntime.New(workerName, runtimeTypes.RuntimeType(rtStr), h.workerRegistry)
	} else {
		var err error
		// TODO: Remove older way of runtime once worker migration is completed
		rt, err = vars.RuntimeFactory.Create("")
		if err != nil {
			logger.ErrorfCtx(ctx, "Could not create runtime client: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error: fmt.Sprintf("Failed to create runtime client: %v", err),
			})

			return
		}
	}

	resp, err := getResourcesResponse(ctx, rt)
	if err != nil {
		logger.ErrorfCtx(ctx, "Could not get system info: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get system information: %v", err),
		})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// getResourcesResponse fetches system info from rt and maps it to ResourcesResponse.
func getResourcesResponse(ctx context.Context, rt runtime.Runtime) (*ResourcesResponse, error) {
	sysInfo, err := rt.GetSystemInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Always initialise the map so the JSON response contains {} rather than null.
	if sysInfo.Accelerators == nil {
		sysInfo.Accelerators = make(map[string]*models.AcceleratorInfo)
	}

	return &ResourcesResponse{
		CPU:          sysInfo.CPU,
		Memory:       sysInfo.Memory,
		Accelerators: sysInfo.Accelerators,
	}, nil
}

// Made with Bob
