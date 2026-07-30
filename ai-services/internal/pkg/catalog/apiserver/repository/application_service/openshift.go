package applicationservice

import (
	"context"
	"errors"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

var errOpenShiftNotSupported = errors.New("OpenShift runtime is not yet supported")

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

func (s *OpenShiftApplicationService) DeleteApplication(_ context.Context, _ uuid.UUID, _ string, _ bool) (*DeleteApplicationResponse, error) {
	return nil, errOpenShiftNotSupported
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the OpenShift runtime executor.
func (s *OpenShiftApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return s.ApplicationServiceBase.CreateApplication(ctx, req, runtimeTypes.RuntimeTypeOpenShift)
}

func (s *OpenShiftApplicationService) GetApplicationResources(_ context.Context, _ uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	return nil, errOpenShiftNotSupported
}

func (s *OpenShiftApplicationService) ApplicationsPs(_ context.Context, _ uuid.UUID) (*types.ApplicationPSResponse, error) {
	return nil, errOpenShiftNotSupported
}

// Made with Bob
