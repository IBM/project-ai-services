package utils

import (
	"fmt"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func GetAllApps(appClient *catalogClient.ApplicationClient) ([]types.Application, error) {
	// List all applications
	listResponse, err := appClient.ListApplications(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch applications: %w", err)
	}

	return listResponse.Data, nil
}

func GetAppByName(appClient *catalogClient.ApplicationClient, appName string) (*types.Application, error) {
	listResponse, err := appClient.ListApplications(nil)
	if err != nil {
		return nil, err
	}
	for _, app := range listResponse.Data {
		if app.Name == appName {
			return &app, nil
		}
	}

	return nil, fmt.Errorf("application with name '%s' not found", appName)
}

// GetAppDetailsWithComponents retrieves full application details including services and components.
// It first finds the app by name, then fetches full details by ID.
func GetAppDetailsWithComponents(appName string) (*types.Application, error) {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create application client: %w", err)
	}

	// First, find the application by name to get its ID
	app, err := GetAppByName(appClient, appName)
	if err != nil {
		return nil, err
	}

	// Then fetch full details including services and components
	appDetails, err := appClient.GetApplication(app.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application details: %w", err)
	}

	return appDetails, nil
}

// GetComponentID extracts the component ID for the specified target from application details.
func GetComponentID(appDetails *types.Application, target string) (string, error) {
	// Search through services and their components
	for _, service := range appDetails.Services {
		for _, component := range service.Component {
			if component.Provider.ID == target {
				return component.ID, nil
			}
		}
	}

	return "", fmt.Errorf("component not found for provider '%s'", target)
}
