package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ResourcesHandler handles resources-related HTTP requests.
type ResourcesHandler struct{}

// NewResourcesHandler creates a new resources handler.
func NewResourcesHandler() *ResourcesHandler {
	return &ResourcesHandler{}
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
//	@Description	Retrieves system resource information including CPU, memory, and accelerator availability
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	ResourcesResponse
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/resources [get]
func (h *ResourcesHandler) GetResources(c *gin.Context) {
	// Create runtime client
	runtimeClient, err := vars.RuntimeFactory.Create("")
	if err != nil {
		logger.ErrorfCtx(c.Request.Context(), "Could not create runtime client: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to create runtime client: %v", err),
		})

		return
	}

	// Get system info from runtime
	sysInfo, err := runtimeClient.GetSystemInfo()
	if err != nil {
		logger.ErrorfCtx(c.Request.Context(), "Could not get system info: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get system information: %v", err),
		})

		return
	}

	// Always initialize accelerators map to ensure it appears in response as empty object
	if sysInfo.Accelerators == nil {
		sysInfo.Accelerators = make(map[string]*models.AcceleratorInfo)
	}

	response := ResourcesResponse{
		CPU:          sysInfo.CPU,
		Memory:       sysInfo.Memory,
		Accelerators: sysInfo.Accelerators,
	}

	c.JSON(http.StatusOK, response)
}

// Made with Bob
