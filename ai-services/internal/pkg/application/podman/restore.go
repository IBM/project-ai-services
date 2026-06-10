package podman

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimePodman "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// Restore restores application data from a backup file for Podman runtime.
func (p *PodmanApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name, 0)
	logger.Infof("Target: %s\n", opts.Target, 0)
	logger.Infof("Backup file: %s\n", opts.BackupFile, 0)

	// Get application details from catalog API using existing utility
	appDetails, err := cliUtils.GetAppDetailsWithComponents(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get application details: %w", err)
	}
	logger.Infof("Application ID: %s\n", appDetails.ID, 0)

	// Get component ID for the target using existing utility
	componentID, err := cliUtils.GetComponentID(appDetails, opts.Target)
	if err != nil {
		return fmt.Errorf("failed to get component ID: %w", err)
	}
	logger.Infof("Component ID: %s\n", componentID, 0)

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		return p.restoreOpenSearch(ctx, componentID, absFilename)
	case "digitize":
		return fmt.Errorf("restore for target 'digitize' is not yet implemented")
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}

// restoreOpenSearch restores OpenSearch data using podman sidecar approach.
func (p *PodmanApplication) restoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	// Get the Podman context from the runtime client
	podmanCtx, err := p.getPodmanContext()
	if err != nil {
		return err
	}

	// Call the OpenSearch-specific restore function
	return restore.RestoreOpenSearch(podmanCtx, templateID, backupFile)
}

// getPodmanContext extracts the Podman context from the runtime client.
func (p *PodmanApplication) getPodmanContext() (context.Context, error) {
	podmanClient, ok := p.runtime.(*runtimePodman.PodmanClient)
	if !ok {
		return nil, fmt.Errorf("runtime is not a Podman client")
	}

	return podmanClient.Context, nil
}

// Made with Bob
