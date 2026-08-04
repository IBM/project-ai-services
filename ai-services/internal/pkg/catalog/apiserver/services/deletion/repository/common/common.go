package common

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type Deletion struct {
	appRepo               dbrepo.ApplicationRepository
	serviceRepo           dbrepo.ServiceRepository
	componentRepo         dbrepo.ComponentRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
}

func NewDeletion(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
) *Deletion {
	return &Deletion{
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
	}
}

// IdentifyOrphanedComponents identifies components that will become orphaned after service deletion.
func (d *Deletion) IdentifyOrphanedComponents(ctx context.Context, appID uuid.UUID, services []models.Service) ([]uuid.UUID, error) {
	serviceIDs := d.buildServiceIDMap(services)

	componentCandidates, err := d.collectComponentCandidates(ctx, appID, services)
	if err != nil {
		return nil, err
	}

	return d.filterOrphanedComponents(ctx, componentCandidates, serviceIDs), nil
}

// buildServiceIDMap creates a map of service IDs for quick lookup.
func (d *Deletion) buildServiceIDMap(services []models.Service) map[uuid.UUID]bool {
	serviceIDs := make(map[uuid.UUID]bool, len(services))
	for _, svc := range services {
		serviceIDs[svc.ID] = true
	}

	return serviceIDs
}

func (d *Deletion) collectComponentCandidates(ctx context.Context, appID uuid.UUID, services []models.Service) (map[uuid.UUID]bool, error) {
	componentCandidates := make(map[uuid.UUID]bool)

	for _, svc := range services {
		deps, err := d.serviceDependencyRepo.GetDependenciesByServiceID(ctx, svc.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "failed to get dependencies for service %s: %s", svc.ID, err)
			_ = catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, models.ApplicationStatusError, "failed to get service dependencies")

			return nil, err
		}

		for _, dep := range deps {
			if dep.DependencyType == models.DependencyTypeComponent {
				componentCandidates[dep.DependencyID] = true
			}
		}
	}

	return componentCandidates, nil
}

// filterOrphanedComponents checks which components are truly orphaned.
func (d *Deletion) filterOrphanedComponents(ctx context.Context, componentCandidates map[uuid.UUID]bool, serviceIDs map[uuid.UUID]bool) []uuid.UUID {
	var orphanedComponents []uuid.UUID

	for componentID := range componentCandidates {
		if d.isComponentOrphaned(ctx, componentID, serviceIDs) {
			orphanedComponents = append(orphanedComponents, componentID)
		}
	}

	return orphanedComponents
}

// isComponentOrphaned checks if a component has no remaining dependent services.
func (d *Deletion) isComponentOrphaned(ctx context.Context, componentID uuid.UUID, serviceIDs map[uuid.UUID]bool) bool {
	dependentServices, err := d.serviceDependencyRepo.GetServicesByDependency(ctx, componentID, models.DependencyTypeComponent)
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to check component %s orphan status: %s", componentID, err)

		return false
	}

	for _, svcID := range dependentServices {
		if !serviceIDs[svcID] {
			return false
		}
	}

	return true
}

// HandleDeletionFailure updates application status when deletion fails.
func (d *Deletion) HandleDeletionFailure(ctx context.Context, appID uuid.UUID, errorMessages []string) {
	errMsg := fmt.Sprintf("Application deletion failed with %d error(s), application not deleted", len(errorMessages))
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	_ = catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, models.ApplicationStatusError, errMsg)
}

func (s *Deletion) HandleStepError(ctx context.Context, appID uuid.UUID, stepContext string, err error) {
	errMsg := fmt.Sprintf("%s: %v", stepContext, err)
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, errMsg); updateErr != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status: %v\n", updateErr)
	}
}
