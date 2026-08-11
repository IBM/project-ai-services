package applicationservice

import (
	"context"
	"errors"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

var errOpenShiftNotSupported = errors.New("OpenShift runtime is not yet supported")

// OpenShiftApplicationService implements ApplicationServiceInterface for the OpenShift runtime.
// It embeds ApplicationServiceBase for all shared DB operations.
type OpenShiftApplicationService struct {
	ApplicationServiceBase
}

func (s *OpenShiftApplicationService) DeleteApplication(_ context.Context, _ uuid.UUID, _ string, _ bool) (*DeleteApplicationResponse, error) {
	return nil, errOpenShiftNotSupported
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
