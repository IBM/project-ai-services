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

func HandleStepError(ctx context.Context, appRepo dbrepo.ApplicationRepository, appID uuid.UUID, stepContext string, err error) {
	errMsg := fmt.Sprintf("%s: %v", stepContext, err)
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, appRepo, appID, models.ApplicationStatusError, errMsg); updateErr != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status: %v\n", updateErr)
	}
}
