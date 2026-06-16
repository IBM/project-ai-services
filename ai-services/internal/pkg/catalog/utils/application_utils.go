package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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
			return fmt.Errorf("invalid application ID: %w", err)
		}
	case uuid.UUID:
		appUUID = id
	default:
		return fmt.Errorf("invalid application ID type: expected string or uuid.UUID")
	}

	// Update the application status in the database
	if err := appRepo.UpdateStatus(ctx, appUUID, status, message); err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}

	return nil
}

// UpdateServiceStatus updates service status in the database.
func UpdateServiceStatus(ctx context.Context, serviceRepo dbrepo.ServiceRepository, serviceID uuid.UUID, status models.ServiceStatus, message string) error {
	if serviceID == uuid.Nil {
		return nil
	}

	if err := serviceRepo.UpdateStatus(ctx, serviceID, status, message); err != nil {
		return fmt.Errorf("failed to update service status: %w", err)
	}

	return nil
}

// UpdateComponentStatus updates component status in the database.
func UpdateComponentStatus(ctx context.Context, componentRepo dbrepo.ComponentRepository, componentID uuid.UUID, status models.ComponentStatus, message string) error {
	if componentID == uuid.Nil {
		return nil
	}

	if err := componentRepo.UpdateStatus(ctx, componentID, status, message); err != nil {
		return fmt.Errorf("failed to update component status: %w", err)
	}

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

// IsNotFoundError checks if an error indicates a resource was not found.
// Returns true for "no such pod", "no such secret", "no such volume" errors.
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()

	return strings.Contains(errMsg, "no such pod") ||
		strings.Contains(errMsg, "no pod with name or ID") ||
		strings.Contains(errMsg, "no such secret") ||
		strings.Contains(errMsg, "no such volume")
}

// CalculateComponentHash creates a unique hash for a component configuration.
// Components with same type, provider, and params will have the same hash.
func CalculateComponentHash(componentType string, providerID string, params map[string]any) string {
	// Create a deterministic string representation
	hashInput := fmt.Sprintf("%s:%s:", componentType, providerID)

	// Sort and add params to ensure consistent hashing
	paramsJSON, _ := json.Marshal(params)
	hashInput += string(paramsJSON)

	// Calculate SHA256 hash
	hash := sha256.Sum256([]byte(hashInput))

	return fmt.Sprintf("%x", hash[:16]) // Use first 16 bytes (32 hex chars)
}

// Made with Bob
