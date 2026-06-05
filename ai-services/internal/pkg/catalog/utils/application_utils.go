package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// GetDeploymentType determines the deployment type based on whether it's an architecture.
func GetDeploymentType(isArchitecture bool) models.DeploymentType {
	if isArchitecture {
		return models.DeploymentTypeArchitectures
	}

	return models.DeploymentTypeServices
}

// UpdateApplicationStatus updates the status and message of an application.
func UpdateApplicationStatus(ctx context.Context, appRepo dbrepo.ApplicationRepository, appID any, status models.ApplicationStatus, message string) error {
	var appUUID uuid.UUID
	var err error

	// Handle both string and UUID types
	switch id := appID.(type) {
	case string:
		appUUID, err = uuid.Parse(id)
		if err != nil {
			log.Printf("Failed to parse application ID %s: %v", id, err)

			return fmt.Errorf("invalid application ID: %w", err)
		}
	case uuid.UUID:
		appUUID = id
	default:
		return fmt.Errorf("invalid application ID type: expected string or uuid.UUID")
	}

	// Update the application status in the database
	if err := appRepo.UpdateStatus(ctx, appUUID, status, message); err != nil {
		log.Printf("Failed to update application %s status in database: %v", appUUID, err)

		return fmt.Errorf("failed to update application status: %w", err)
	}

	log.Printf("Application %s status updated: %s - %s", appUUID, status, message)

	return nil
}

// BuildExternalURL constructs an HTTPS URL from a domain and optional port.
// If the port is not the default HTTPS port (443), it appends the port to the URL.
func BuildExternalURL(domain string, httpsPort string) string {
	url := fmt.Sprintf("https://%s", domain)
	if httpsPort != constants.DefaultHTTPSPort {
		url = url + ":" + httpsPort
	}

	return url
}

// GenerateInstanceSlug creates a short slug from an ID using SHA256 hash.
// Returns the first 10 characters of the hex-encoded hash.
// This is used to create consistent directory names for applications and components.
func GenerateInstanceSlug(id string) string {
	hash := sha256.Sum256([]byte(id))
	hexHash := hex.EncodeToString(hash[:])

	return hexHash[:10]
}

// Made with Bob
