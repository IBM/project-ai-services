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

// HandleDeletionFailure updates application status when deletion fails.
func HandleDeletionFailure(ctx context.Context, appRepo dbrepo.ApplicationRepository, appID uuid.UUID, errorMessages []string) {
	errMsg := fmt.Sprintf("Application deletion failed with %d error(s), application not deleted", len(errorMessages))
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	_ = catalogutils.UpdateApplicationStatus(ctx, appRepo, appID, models.ApplicationStatusError, errMsg)
}

// CredentialRevoker revokes the service credentials of a deployment.
type CredentialRevoker interface {
	RevokeForDeployment(ctx context.Context, deploymentID string) error
}

// RevokeServiceCredentials revokes every credential of a deleted service. It does nothing
// without a revoker. A failure is logged and not returned: the service is already gone and a
// credential that could not be revoked still expires on its own.
func RevokeServiceCredentials(ctx context.Context, r CredentialRevoker, serviceID uuid.UUID) {
	if r == nil {
		return
	}
	if err := r.RevokeForDeployment(ctx, serviceID.String()); err != nil {
		logger.WarningfCtx(ctx, "service %s: could not revoke its service credentials: %v", serviceID, err)
	}
}

func HandleStepError(ctx context.Context, appRepo dbrepo.ApplicationRepository, appID uuid.UUID, stepContext string, err error) {
	errMsg := fmt.Sprintf("%s: %v", stepContext, err)
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, appRepo, appID, models.ApplicationStatusError, errMsg); updateErr != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status: %v\n", updateErr)
	}
}
