package appcreate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ModelRequest is one `--model <component_type>=<value>` flag: the model the user wants for a
// component type. The provider is chosen separately, with --params.
type ModelRequest struct {
	componentType string
	value         string
}

// ParseModelFlags parses the values of the --model flag. Each is <component_type>=<value>; a
// component type may be given only once.
func ParseModelFlags(raw []string) ([]ModelRequest, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]ModelRequest, 0, len(raw))

	for _, entry := range raw {
		componentType, value, ok := strings.Cut(entry, "=")
		componentType, value = strings.TrimSpace(componentType), strings.TrimSpace(value)

		if !ok || componentType == "" || value == "" {
			return nil, fmt.Errorf("invalid model %q: expected <component_type>=<model>, e.g. llm=ibm-granite/granite-4.1-8b-fp8", entry)
		}

		if seen[componentType] {
			return nil, fmt.Errorf("component_type %q is given more than once", componentType)
		}

		seen[componentType] = true
		out = append(out, ModelRequest{componentType: componentType, value: value})
	}

	return out, nil
}

// ModelAPI is what model resolution needs from the catalog API. *catalogClient.ModelClient
// implements it.
type ModelAPI interface {
	ListModels(ctx context.Context, params *catalogClient.ListModelsParams) ([]catalogClient.Model, error)
	ListCatalogModels(ctx context.Context, componentType string) ([]catalogTypes.ModelCatalogEntry, error)
}

// bindingKind is how a component type gets its model.
type bindingKind int

const (
	// bindNew deploys a new model from the catalog.
	bindNew bindingKind = iota
	// bindInstance attaches to a registered local model that is running.
	bindInstance
	// bindConnector uses a registered remote model through its connector.
	bindConnector
)

// modelBinding is the model a component type resolved to.
type modelBinding struct {
	kind          bindingKind
	componentType string
	// model is the catalog model ID: the model to deploy for bindNew, the model the registered
	// model serves otherwise.
	model string
	// providers are the providers the catalog lists for the model. Set for bindNew.
	providers []catalogTypes.ModelCatalogProvider
	// registered is the model of the caller to attach to. Set for bindInstance and bindConnector.
	registered *catalogClient.Model
}

// ModelResolver turns the value of a --model flag into a modelBinding.
type ModelResolver struct {
	api     ModelAPI
	catalog map[string][]catalogTypes.ModelCatalogEntry // catalog models by component type, fetched once
}

// NewModelResolver returns a ModelResolver that reads models from api.
func NewModelResolver(api ModelAPI) *ModelResolver {
	return &ModelResolver{api: api, catalog: make(map[string][]catalogTypes.ModelCatalogEntry)}
}

// resolve resolves the value of req, in this order:
//  1. a catalog model ID deploys that model anew;
//  2. anything else is the name of a registered model, which is reused.
//
// The server refuses a registered model name equal to a catalog model ID, so a value never matches
// both.
func (r *ModelResolver) resolve(ctx context.Context, req ModelRequest) (modelBinding, error) {
	entries, err := r.catalogEntries(ctx, req.componentType)
	if err != nil {
		return modelBinding{}, err
	}

	if entry := findCatalogEntry(entries, req.value); entry != nil {
		return modelBinding{kind: bindNew, componentType: req.componentType, model: entry.ID, providers: entry.Providers}, nil
	}

	registered, err := r.registeredNamed(ctx, req.value)
	if err != nil {
		return modelBinding{}, err
	}

	if registered == nil {
		return modelBinding{}, r.noMatchError(ctx, req, entries)
	}

	return registeredBinding(req.componentType, registered), nil
}

// registeredNamed returns the registered model of the caller with the given name, or nil. Names
// are unique, and compared without regard to case as the API does.
func (r *ModelResolver) registeredNamed(ctx context.Context, name string) (*catalogClient.Model, error) {
	models, err := r.api.ListModels(ctx, &catalogClient.ListModelsParams{Search: name})
	if err != nil {
		return nil, fmt.Errorf("failed to list registered models: %w", err)
	}

	for i := range models {
		if strings.EqualFold(models[i].Name, name) {
			return &models[i], nil
		}
	}

	return nil, nil
}

// noMatchError is the error for a value that is no registered model and no catalog model. It lists
// what could have been given.
func (r *ModelResolver) noMatchError(ctx context.Context, req ModelRequest, entries []catalogTypes.ModelCatalogEntry) error {
	var sb strings.Builder

	fmt.Fprintf(&sb, "no model matches %q for component_type %q", req.value, req.componentType)

	sb.WriteString("\n  catalog models (deploys a new one):")

	if len(entries) == 0 {
		sb.WriteString("\n    none")
	}

	for _, e := range entries {
		fmt.Fprintf(&sb, "\n    - %s", e.ID)
	}

	sb.WriteString("\n  registered models (reuses it, by name):")

	registered, err := r.api.ListModels(ctx, &catalogClient.ListModelsParams{ComponentType: req.componentType})
	if err != nil || len(registered) == 0 {
		sb.WriteString("\n    none")
	}

	for _, m := range registered {
		fmt.Fprintf(&sb, "\n    - %s (%s, %s)", m.Name, m.DeploymentType, m.Status)
	}

	return fmt.Errorf("%s", sb.String())
}

// registeredBinding binds a component type to a registered model. Whether the model is of that
// component type and can be attached is left to the API.
func registeredBinding(componentType string, m *catalogClient.Model) modelBinding {
	kind := bindInstance
	if m.DeploymentType == catalogClient.ModelDeploymentRemote {
		kind = bindConnector
	}

	return modelBinding{kind: kind, componentType: componentType, model: m.Model, registered: m}
}

// defaultModel returns the model a local provider deploys when none is chosen: the one its
// models.yaml marks as the default.
func (r *ModelResolver) defaultModel(ctx context.Context, componentType, providerID string) (string, error) {
	entries, err := r.catalogEntries(ctx, componentType)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		for _, p := range e.Providers {
			if p.ID == providerID && p.DeploymentType == catalogConstants.DeploymentTypeLocal && p.DefaultModel {
				return e.ID, nil
			}
		}
	}

	return "", fmt.Errorf("provider %q has no default %s model; choose one with --model %s=<model>", providerID, componentType, componentType)
}

// catalogEntries returns the catalog models of a component type, fetching them once.
func (r *ModelResolver) catalogEntries(ctx context.Context, componentType string) ([]catalogTypes.ModelCatalogEntry, error) {
	if entries, ok := r.catalog[componentType]; ok {
		return entries, nil
	}

	entries, err := r.api.ListCatalogModels(ctx, componentType)
	if err != nil {
		return nil, fmt.Errorf("failed to list catalog models for component_type %q: %w", componentType, err)
	}

	r.catalog[componentType] = entries

	return entries, nil
}

// findCatalogEntry returns the entry with the given model ID, or nil.
func findCatalogEntry(entries []catalogTypes.ModelCatalogEntry, id string) *catalogTypes.ModelCatalogEntry {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}

	return nil
}

// offersLocally reports whether a catalog model can be deployed through the local provider.
func offersLocally(providers []catalogTypes.ModelCatalogProvider, providerID string) bool {
	return slices.ContainsFunc(providers, func(p catalogTypes.ModelCatalogProvider) bool {
		return p.ID == providerID && p.DeploymentType == catalogConstants.DeploymentTypeLocal
	})
}
