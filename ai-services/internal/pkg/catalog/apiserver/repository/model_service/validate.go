package modelservice

import (
	"context"
	"fmt"
	"regexp"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

const (
	minNameLen = 3
	maxNameLen = 100
)

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateCreateRequest performs the provider-independent checks on a create request.
// It is a pure function so it can be tested without any service dependencies.
func validateCreateRequest(req apimodels.CreateModelRequest) error {
	switch {
	case !req.DeploymentType.IsValid():
		return validators.BadRequest("invalid deployment_type %q; must be 'local' or 'remote'", req.DeploymentType)
	case !validName(req.Name):
		return validators.BadRequest("name must be between %d and %d characters and contain only letters, numbers, hyphens, and underscores", minNameLen, maxNameLen)
	case req.ProviderID == "":
		return validators.BadRequest("provider_id is required")
	case req.Model == "":
		return validators.BadRequest("model is required")
	default:
		return nil
	}
}

func validName(name string) bool {
	return len(name) >= minNameLen && len(name) <= maxNameLen && nameRegex.MatchString(name)
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
