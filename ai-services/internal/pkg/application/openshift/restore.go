package openshift

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	commonrestore "github.com/project-ai-services/ai-services/internal/pkg/application/common/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Restore restores application data from a backup file for OpenShift runtime.
func (o *OpenshiftApplication) Restore(ctx context.Context, opts types.RestoreOptions) error {
	logger.Infof("Starting restore for application: %s\n", opts.Name)
	logger.Infof("Target: %s\n", opts.Target)
	logger.Infof("Backup file: %s\n", opts.BackupFile)

	// For OpenShift, use the name as-is (namespace convention)
	applicationID := opts.Name

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(opts.BackupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Execute restore based on target
	switch opts.Target {
	case "opensearch":
		return restore.RestoreOpenSearch(ctx, applicationID, absFilename)
	case "digitize":
		return o.restoreDigitize(ctx, opts.Name, absFilename)
	default:
		return fmt.Errorf("unsupported target: %s", opts.Target)
	}
}

// restoreDigitize restores digitize metadata using the Import API for OpenShift.
func (o *OpenshiftApplication) restoreDigitize(ctx context.Context, appName, backupFile string) error {
	logger.Infof("Restoring digitize metadata\n")
	logger.Infof("Digitize Import (API-based Approach)\n")

	importPayload, err := commonrestore.GetDigitizeData(backupFile)
	if err != nil {
		return err
	}

	// Get digitize service API URL from OpenShift routes
	digitizeURL, err := o.getDigitizeAPIURL(ctx, appName)
	if err != nil {
		return err
	}

	logger.Infof("Digitize API URL: %s\n", digitizeURL)

	// Create digitize restore client and call Import API
	client := commonrestore.NewDigitizeRestoreClient(digitizeURL)
	if err := client.CallImportAPI(importPayload); err != nil {
		return err
	}

	logger.Infof("✓ Digitize metadata restore completed successfully\n")

	return nil
}

// getDigitizeAPIURL retrieves the digitize API URL from OpenShift routes.
func (o *OpenshiftApplication) getDigitizeAPIURL(ctx context.Context, appName string) (string, error) {
	logger.Infof("Fetching digitize route from OpenShift...\n")

	// List all routes in the namespace using the runtime interface
	routes, err := o.runtime.ListRoutes()
	if err != nil {
		return "", fmt.Errorf("failed to list routes: %w", err)
	}

	// Find the digitize-api route
	// Route naming convention: digitize-api
	var digitizeRoute string
	for _, route := range routes {
		// Check if route name is "digitize-api"
		routeName := strings.ToLower(route.Name)
		if routeName == "digitize-api" {
			digitizeRoute = route.HostPort

			break
		}
	}

	if digitizeRoute == "" {
		return "", fmt.Errorf("digitize-api route not found. Please ensure the digitize-api route exists in the namespace")
	}

	// Construct the full API URL with https scheme
	apiURL := fmt.Sprintf("https://%s", digitizeRoute)

	return apiURL, nil
}

// Made with Bob
