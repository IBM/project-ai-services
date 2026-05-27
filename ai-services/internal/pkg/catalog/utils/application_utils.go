package utils

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
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

// Made with Bob
