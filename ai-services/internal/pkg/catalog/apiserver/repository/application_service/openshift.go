package applicationservice

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

const errOpenShiftNotSupported = "OpenShift runtime is not yet supported"

type OpenShiftApplicationService struct{}

func (s *OpenShiftApplicationService) ListApplications(_ context.Context, _ ListApplicationsRequest) (*types.ApplicationListResponse, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) DeleteApplication(_ context.Context, _ uuid.UUID, _ string, _ bool) (*DeleteApplicationResponse, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) CreateApplication(_ context.Context, _ apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) UpdateApplication(_ context.Context, _ uuid.UUID, _, _ string) (*types.Application, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) GetApplicationByID(_ context.Context, _ uuid.UUID) (*types.Application, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) GetApplicationResources(_ context.Context, _ uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

func (s *OpenShiftApplicationService) ApplicationsPs(_ context.Context, _ uuid.UUID) (*types.ApplicationPSResponse, error) {
	return nil, fmt.Errorf(errOpenShiftNotSupported)
}

// Made with Bob
