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
