package applicationservice

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	consts "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// PodmanApplicationService implements ApplicationServiceInterface for the Podman runtime.
// It embeds ApplicationServiceBase for all shared DB operations and adds Podman-specific
// deployment, pod-status, and resource-inspection logic.
type PodmanApplicationService struct {
	ApplicationServiceBase
}

// DeleteApplication initiates async deletion of an application and returns immediately.
func (s *PodmanApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	return s.ApplicationServiceBase.DeleteApplication(ctx, id, user, keepData, runtimeTypes.RuntimeTypePodman)
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the Podman runtime executor.
func (s *PodmanApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return s.ApplicationServiceBase.CreateApplication(ctx, req, runtimeTypes.RuntimeTypePodman)
}

// ApplicationsPs retrieves pod/container status by querying Podman.
func (s *PodmanApplicationService) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
	return s.ApplicationServiceBase.ApplicationsPs(ctx, appID, "")
}

// GetApplicationResources retrieves CPU, memory, and Spyre-card usage by querying Podman pods.
func (s *PodmanApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	// Podman has no per-app namespace; pass empty string so the runtime factory
	// creates a client without namespace context.
	return s.ApplicationServiceBase.GetApplicationResources(ctx, id, "")
}

// Made with Bob
