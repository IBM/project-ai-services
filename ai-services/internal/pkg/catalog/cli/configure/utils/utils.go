package utils

import (
	"context"
	"fmt"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// CollectAdminPassword prompts the operator for the admin password.
//
// Fresh install (secretExists=false): prompts with confirmation and returns the
// bcrypt hash alongside the plaintext so the caller can both store the secret
// and log in to the running catalog.
//
// Reconfigure (secretExists=true): prompts without confirmation, returns an
// empty hash (the secret already exists) and the plaintext for login.
func CollectAdminPassword(secretExists bool) (passwordHash, adminPassword string, err error) {
	if !secretExists {
		return catalogutils.PromptNewAdminPassword()
	}

	adminPassword, err = catalogutils.PromptExistingAdminPassword()

	return "", adminPassword, err
}

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
