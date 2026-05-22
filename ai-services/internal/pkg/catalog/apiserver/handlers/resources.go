package handlers

import (
	"fmt"
	"net/http"

	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

const (
	// percentageDivisor is used to convert percentage to decimal.
	percentageDivisor = 100.0
)

// ResourcesHandler handles resources-related HTTP requests.
type ResourcesHandler struct{}

// NewResourcesHandler creates a new resources handler.
func NewResourcesHandler() *ResourcesHandler {
	return &ResourcesHandler{}
}

// CPUInfo represents CPU utilization information.
type CPUInfo struct {
	TotalCores     int     `json:"total_cores"`
	AvailableCores float64 `json:"available_cores"`
}

// MemoryInfo represents memory usage information.
type MemoryInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

// AcceleratorInfo represents accelerator availability information.
type AcceleratorInfo struct {
	Total     int `json:"total"`
	Available int `json:"available"`
}

// ResourcesResponse represents system resource information.
type ResourcesResponse struct {
	CPU          *CPUInfo                    `json:"cpu,omitempty"`
	Memory       *MemoryInfo                 `json:"memory,omitempty"`
	Accelerators map[string]*AcceleratorInfo `json:"accelerators,omitempty"`
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
	response := ResourcesResponse{}

	// Get Podman system info for CPU and memory
	cpuInfo, memInfo, err := h.getPodmanSystemInfo()
	if err != nil {
		logger.Errorf("Could not get Podman system info: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("Failed to get system information: %v", err),
		})

		return
	}

	response.CPU = cpuInfo
	response.Memory = memInfo

	// Get accelerator information (Spyre cards)
	acceleratorInfo := h.getAcceleratorInfo()
	response.Accelerators = acceleratorInfo

	c.JSON(http.StatusOK, response)
}

// getPodmanSystemInfo retrieves CPU and memory information from Podman.
func (h *ResourcesHandler) getPodmanSystemInfo() (*CPUInfo, *MemoryInfo, error) {
	// Create Podman client
	client, err := podman.NewPodmanClient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Podman client: %w", err)
	}

	// Get system info
	info, err := system.Info(client.Context, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get system info: %w", err)
	}

	// Extract CPU information
	var cpuInfo *CPUInfo
	if info.Host != nil {
		totalCores := int(info.Host.CPUs)
		idlePercent := 0.0

		if info.Host.CPUUtilization != nil {
			idlePercent = info.Host.CPUUtilization.IdlePercent
		}

		// Calculate available cores: available_cores = (total_cores * idle_percent) / 100
		availableCores := (float64(totalCores) * idlePercent) / percentageDivisor

		cpuInfo = &CPUInfo{
			TotalCores:     totalCores,
			AvailableCores: availableCores,
		}
	}

	// Extract memory information
	var memInfo *MemoryInfo
	if info.Host != nil {
		totalBytes := info.Host.MemTotal
		availableBytes := info.Host.MemFree

		memInfo = &MemoryInfo{
			TotalBytes:     totalBytes,
			AvailableBytes: availableBytes,
		}
	}

	return cpuInfo, memInfo, nil
}

// getAcceleratorInfo retrieves accelerator availability information.
func (h *ResourcesHandler) getAcceleratorInfo() map[string]*AcceleratorInfo {
	accelerators := make(map[string]*AcceleratorInfo)

	// Get total Spyre cards
	totalCards, err := helpers.ListSpyreCards()
	if err != nil {
		logger.Errorf("Could not list Spyre cards: %v", err)
		accelerators["ibm.com/spyre_pf"] = &AcceleratorInfo{
			Total:     0,
			Available: 0,
		}

		return accelerators
	}

	totalCount := len(totalCards)
	if totalCount == 0 {
		accelerators["ibm.com/spyre_pf"] = &AcceleratorInfo{
			Total:     0,
			Available: 0,
		}

		return accelerators
	}

	// Get available Spyre cards
	availableCards, err := helpers.FindFreeSpyreCards()
	if err != nil {
		logger.Errorf("Could not find available Spyre cards: %v", err)
		accelerators["ibm.com/spyre_pf"] = &AcceleratorInfo{
			Total:     totalCount,
			Available: 0,
		}

		return accelerators
	}

	availableCount := len(availableCards)

	accelerators["ibm.com/spyre_pf"] = &AcceleratorInfo{
		Total:     totalCount,
		Available: availableCount,
	}

	return accelerators
}

// Made with Bob
