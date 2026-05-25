package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
//	@Description	Retrieves system resource information including CPU, memory, and Spyre card availability (Podman environments only)
//	@Tags			Catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	ResourcesResponse
//	@Failure		401	{object}	ErrorResponse	"Unauthorized - Invalid or missing access token"
//	@Failure		500	{object}	ErrorResponse	"Internal Server Error"
//	@Router			/resources [get]
func (h *ResourcesHandler) GetResources(c *gin.Context) {
	// Get runtime from global factory
	runtime := vars.RuntimeFactory.GetRuntimeType()

	// Create runtime client
	runtimeClient, err := vars.RuntimeFactory.Create("")
	if err != nil {
		logger.Errorf("Could not create runtime client: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to create runtime client: %v", err),
		})

		return
	}

	// Get system info from runtime
	sysInfo, err := runtimeClient.GetSystemInfo()
	if err != nil {
		logger.Errorf("Could not get system info: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get system information: %v", err),
		})

		return
	}

	// For Podman runtime, populate accelerator information (Spyre cards)
	if runtime == types.RuntimeTypePodman {
		sysInfo.Accelerators = h.getAcceleratorInfo()
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

// getAcceleratorInfo retrieves accelerator availability information for Podman.
func (h *ResourcesHandler) getAcceleratorInfo() map[string]*models.AcceleratorInfo {
	accelerators := make(map[string]*models.AcceleratorInfo)

	// Get total Spyre cards
	totalCards, err := helpers.ListSpyreCards()
	if err != nil {
		logger.Errorf("Could not list Spyre cards: %v", err)
		// Return empty map when error occurs
		return accelerators
	}

	totalCount := len(totalCards)
	if totalCount == 0 {
		// Return empty map when no Spyre cards found
		return accelerators
	}

	// Get available Spyre cards
	availableCards, err := helpers.FindFreeSpyreCards()
	if err != nil {
		logger.Errorf("Could not find available Spyre cards: %v", err)
		accelerators["ibm.com/spyre_pf"] = &models.AcceleratorInfo{
			Total:     totalCount,
			Available: 0,
		}

		return accelerators
	}

	availableCount := len(availableCards)

	accelerators["ibm.com/spyre_pf"] = &models.AcceleratorInfo{
		Total:     totalCount,
		Available: availableCount,
	}

	return accelerators
}

// Made with Bob
