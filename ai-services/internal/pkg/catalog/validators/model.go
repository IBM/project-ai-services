package validators

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ValidateModelDeployable checks that model is in the catalog and can serve componentType through
// providerID with the given deployment type. Failures are 400s that say what is available. It is
// the one check behind both POST /api/v1/models and the model components of an application.
func ValidateModelDeployable(
	ctx context.Context,
	provider *catalog.CatalogProvider,
	model, componentType, providerID string,
	deploymentType constants.DeploymentType,
) error {
	options, err := provider.GetModelDeployOptions(ctx, model, "")
	if errors.Is(err, catalog.ErrModelNotFound) {
		return BadRequest("invalid model %q; it is not in the catalog", model)
	}

	if err != nil {
		return fmt.Errorf("failed to get deploy options of model %q: %w", model, err)
	}

	return CheckModelDeployOptions(options, model, componentType, providerID, deploymentType)
}

// CheckModelDeployOptions checks that the model described by options can serve the component type
// and be deployed with the provider and deployment type.
func CheckModelDeployOptions(
	options *types.ModelDeployOptions,
	model, componentType, providerID string,
	deploymentType constants.DeploymentType,
) error {
	if options.ComponentType.ID != componentType {
		return BadRequest("model %q cannot be used as component_type %q; it is a model of component_type: %s",
			model, componentType, options.ComponentType.ID)
	}

	if len(options.Providers) == 0 {
		return BadRequest("model %q is not available for any provider", model)
	}

	offered := make([]string, 0, len(options.Providers))

	for _, p := range options.Providers {
		if p.ID == providerID && p.DeploymentType == deploymentType {
			return nil
		}

		offered = append(offered, fmt.Sprintf("%s (%s)", p.ID, p.DeploymentType))
	}

	return BadRequest("model %q is not available for %s provider %q; it is offered by: %s",
		model, deploymentType, providerID, strings.Join(offered, ", "))
}

// validateModelComponentFields checks the shape of a component entry of a model type: either a
// new deployment (provider_id, version and model, no instance_id) or an existing instance
// (instance_id alone). Whether the instance or the model actually exist is checked later, against
// the DB and the catalog.
func validateModelComponentFields(c apimodels.Component) error {
	if c.InstanceID != "" {
		if c.ProviderID != "" || c.Version != "" || c.Model != "" || len(c.Params) > 0 {
			return BadRequest(
				"component '%s' sets instance_id, so provider_id, version, model and params must be omitted; they come from the instance",
				c.ComponentType)
		}

		return nil
	}

	if c.ProviderID == "" || c.Version == "" {
		return BadRequest("component '%s' requires provider_id and version, or an instance_id", c.ComponentType)
	}

	if c.Model == "" {
		return BadRequest("component '%s' requires a model, or an instance_id", c.ComponentType)
	}

	if _, ok := c.Params[constants.ParamKeyModel]; ok {
		return BadRequest("component '%s': model is a top-level field, not a param", c.ComponentType)
	}

	return nil
}

// validateNonModelComponentFields checks the shape of a component entry of a non-model type:
// provider_id and version are required, model and instance_id do not apply.
func validateNonModelComponentFields(c apimodels.Component) error {
	if c.ProviderID == "" || c.Version == "" {
		return BadRequest("component '%s' requires provider_id and version", c.ComponentType)
	}

	if c.Model != "" || c.InstanceID != "" {
		return BadRequest("component '%s' is not a model component; model and instance_id do not apply", c.ComponentType)
	}

	return nil
}
