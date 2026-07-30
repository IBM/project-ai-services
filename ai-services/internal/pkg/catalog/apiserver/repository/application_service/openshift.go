package applicationservice

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// OpenShiftApplicationService implements ApplicationServiceInterface for the OpenShift runtime.
// It embeds ApplicationServiceBase for all shared DB operations.
//
// NOTE: When CreateApplication is implemented here, wire in a DeploymentRegistry (same pattern
// as PodmanApplicationService) so that mid-deployment deletion works on OCP too:
//   1. Add deploymentRegistry *DeploymentRegistry field
//   2. Call registry.Register in executeDeploymentAsync
//   3. Call registry.Cancel in DeleteApplication before firing PerformDeletion
type OpenShiftApplicationService struct {
	ApplicationServiceBase
}

func (s *OpenShiftApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	return s.ApplicationServiceBase.DeleteApplication(ctx, id, user, keepData, runtimeTypes.RuntimeTypeOpenShift)
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the OpenShift runtime executor.
func (s *OpenShiftApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return s.ApplicationServiceBase.CreateApplication(ctx, req, runtimeTypes.RuntimeTypeOpenShift)
}

// GetApplicationResources retrieves CPU, memory, and Spyre-card usage for an application
// using the OpenShift runtime. Each application is deployed into its own namespace
// (ai-services-<first 8 chars of UUID>), so the runtime client is created with that namespace.
func (s *OpenShiftApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	return s.ApplicationServiceBase.GetApplicationResources(ctx, id, catalogutils.AppNamespace(id))
}

// ApplicationsPs retrieves pod/container status by querying the application's OpenShift namespace.
func (s *OpenShiftApplicationService) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
	return s.ApplicationServiceBase.ApplicationsPs(ctx, appID, catalogutils.AppNamespace(appID))
}

// Made with Bob
