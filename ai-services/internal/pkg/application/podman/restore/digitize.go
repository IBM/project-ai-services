package restore

import (
	"fmt"

	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// GetDigitizeAPIURL extracts the digitize API URL from application details.
func GetDigitizeAPIURL(appDetails *catalogTypes.Application) (string, error) {
	// Search through services to find digitize service
	for _, service := range appDetails.Services {
		if service.Type == "digitize" {
			// Look for API endpoint
			for _, endpoint := range service.Endpoints {
				if endpointType, ok := endpoint["type"].(string); ok && endpointType == "api" {
					if url, ok := endpoint["url"].(string); ok && url != "" {
						return url, nil
					}
				}
			}

			return "", fmt.Errorf("digitize service found but no API endpoint available")
		}
	}

	return "", fmt.Errorf("digitize service not found in application")
}

// Made with Bob
