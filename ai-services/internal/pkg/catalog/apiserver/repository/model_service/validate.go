package modelservice

import (
	"context"
	"fmt"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// validateCreateRequest performs the checks on a create request that are not expressed as
// binding tags on apimodels.CreateModelRequest (required fields, name length and deployment_type
// are enforced there). It is a pure function so it can be tested without any service dependencies.
func validateCreateRequest(req apimodels.CreateModelRequest) error {
	if err := validators.ValidateResourceName("Model name", req.Name); err != nil {
		return err
	}

	// TODO(model-validation): validate req.Model against the catalog models (the upcoming
	// GET /catalog/models listing, or the findModel helper) and reject unknown models with a 400.
	// The model is no longer part of the provider params schema, so nothing validates it today.

	return nil
}

// ensureNameAvailable rejects a name already used by a local model (component) or a remote
// model connector. Names share one namespace because both are listed together by GET /models.
func (s *ModelService) ensureNameAvailable(ctx context.Context, name string) error {
	component, err := s.componentRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to check component name uniqueness: %w", err)
	}
	if component != nil {
		return validators.Conflict("A model with name %q already exists", name)
	}

	connector, err := s.connectorRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to check connector name uniqueness: %w", err)
	}
	if connector != nil {
		return validators.Conflict("A connector with name %q already exists", name)
	}

	return nil
}
