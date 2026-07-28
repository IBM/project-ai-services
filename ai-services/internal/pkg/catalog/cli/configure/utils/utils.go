package utils

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ConfirmCatalogReset displays a warning about catalog service unavailability and prompts for user confirmation.
// The flagName parameter is used to customize the warning and confirmation messages.
// Returns true if user confirms, false if cancelled, or an error if confirmation fails.
func ConfirmCatalogReset(flagName string) (bool, error) {
	logger.WarningfCtx(context.Background(), "Resetting %s will reload the catalog pod, catalog service will be temporarily unavailable during this time!", flagName)

	// Confirm action
	confirmed, err := utils.ConfirmAction(fmt.Sprintf("\nDo you want to continue, with %s reset?", flagName))
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfofCtx(context.Background(), "Catalog %s reset cancelled", flagName)

		return false, nil
	}

	return true, nil
}
