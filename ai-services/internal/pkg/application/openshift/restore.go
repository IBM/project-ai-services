package openshift

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	commonrestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/restore"
	podmanrestore "github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores OpenSearch and digitize data from a backup file for OpenShift runtime.
// Older single-part archives are supported: only the parts present in the archive are restored.
func (o *OpenshiftApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name)
	logger.Infof("Backup file: %s\n", opts.BackupFile)

	// Get application details from catalog API
	appDetails, err := cliUtils.GetAppDetailsWithComponents(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID)

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	rootDir, cleanup, err := podmanrestore.ExtractBackup(absFilename)
	if err != nil {
		return err
	}
	defer cleanup()

	opensearchDir, digitizeDir, err := podmanrestore.LocateBackupParts(rootDir)
	if err != nil {
		return err
	}

	if opensearchDir != "" {
		// Get component ID (templateID) — used as ai-services.io/template label selector
		templateID, err := cliUtils.GetComponentID(appDetails, "opensearch")
		if err != nil {
			return fmt.Errorf("failed to get opensearch component ID: %w", err)
		}
		logger.Infof("OpenSearch component ID: %s\n", templateID)

		// Derive namespace: "ai-services-<first 8 chars of app UUID>"
		appUUID, err := uuid.Parse(appDetails.ID)
		if err != nil {
			return fmt.Errorf("failed to parse application UUID: %w", err)
		}
		namespace := catalogUtils.AppNamespace(appUUID)
		logger.Infof("Namespace: %s\n", namespace)

		if err := restore.RestoreOpenSearch(ctx, templateID, namespace, opensearchDir); err != nil {
			return fmt.Errorf("opensearch restore failed: %w", err)
		}
	}

	if digitizeDir != "" {
		if err := commonrestore.RestoreDigitize(ctx, appDetails, digitizeDir); err != nil {
			return fmt.Errorf("digitize restore failed: %w", err)
		}
	}

	return nil
}

// Made with Bob
