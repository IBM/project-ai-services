// Package appcreate builds the catalog API request of 'ai-services application create': it turns
// the user's --params and --model flags and the template's deploy options into a create request.
// It makes no API calls itself: the catalog API is reached through the small interfaces it declares.
package appcreate

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// modelParamKey is the param a model used to be passed in. The model is a top-level field of a
// component now, chosen with --model.
const modelParamKey = "model"

// DeployOptionsFetcher is what the request builder needs from the catalog API.
// *catalogClient.ApplicationClient implements it.
type DeployOptionsFetcher interface {
	GetServiceDeployOptions(ctx context.Context, serviceID, runtimeType string) (*catalogTypes.DeployOptionsService, error)
	GetArchitectureDeployOptions(ctx context.Context, architectureID, runtimeType string) (*catalogTypes.DeployOptionsArchitecture, error)
	GetComponentProviderParams(ctx context.Context, componentType, providerID, runtimeType string) (map[string]any, error)
}

// Options are the user's choices that shape a create request.
type Options struct {
	// Runtime is the runtime type of the target worker.
	Runtime runtimeTypes.RuntimeType
	// WorkerName is the worker to deploy to.
	WorkerName string
	// Params are the parsed --params.
	Params map[string]string
	// Models are the parsed --model flags.
	Models []ModelRequest
}

// Result is a create request with what the user should see before it is sent.
type Result struct {
	// Request is the request to send.
	Request *catalogClient.CreateApplicationRequest
	// Selections say what each model component type resolved to.
	Selections []string
	// Hints are advice about the choices made.
	Hints []string
}

// componentTypeProvider identifies a model component type served by a provider.
type componentTypeProvider struct {
	componentType, provider string
}

// Builder builds the create request of a service or an architecture. It is single use: it keeps
// the choices made while building, so build one request per Builder.
type Builder struct {
	deploy DeployOptionsFetcher
	models *ModelResolver
	opts   Options

	// bindings are the models chosen with --model, by component type. Set by resolveModels.
	bindings map[string]modelBinding
	// chosen is the model picked for a component type and provider when --model does not say. Services of an
	// architecture that share a component type and provider must share the model, or the server rejects the
	// request; the first service to pick decides.
	chosen map[componentTypeProvider]string
	report Result
}

// NewBuilder returns a Builder that reads deploy options from deploy and resolves models with models.
func NewBuilder(deploy DeployOptionsFetcher, models *ModelResolver, opts Options) *Builder {
	return &Builder{
		deploy:   deploy,
		models:   models,
		opts:     opts,
		bindings: make(map[string]modelBinding),
		chosen:   make(map[componentTypeProvider]string),
	}
}

// BuildService builds the request that deploys a standalone service.
func (b *Builder) BuildService(ctx context.Context, serviceID, appName string) (*Result, error) {
	options, err := b.deploy.GetServiceDeployOptions(ctx, serviceID, string(b.opts.Runtime))
	if err != nil {
		return nil, fmt.Errorf("failed to get deploy options: %w", err)
	}

	entries, err := b.buildServices(ctx, []catalogTypes.DeployOptionsService{*options})
	if err != nil {
		return nil, err
	}

	b.report.Request = &catalogClient.CreateApplicationRequest{
		CatalogID:  serviceID,
		Name:       appName,
		Services:   entries,
		Version:    entries[0].Version,
		WorkerName: b.opts.WorkerName,
	}

	return &b.report, nil
}

// BuildArchitecture builds the request that deploys an architecture.
func (b *Builder) BuildArchitecture(ctx context.Context, arch *catalogTypes.Architecture, appName string) (*Result, error) {
	options, err := b.deploy.GetArchitectureDeployOptions(ctx, arch.ID, string(b.opts.Runtime))
	if err != nil {
		return nil, fmt.Errorf("failed to get deploy options: %w", err)
	}

	services := make([]catalogTypes.DeployOptionsService, 0, len(arch.Services))

	for _, ref := range arch.Services {
		i := slices.IndexFunc(options.Services, func(s catalogTypes.DeployOptionsService) bool { return s.ID == ref.ID })
		if i < 0 {
			return nil, fmt.Errorf("deploy options not found for service '%s'", ref.ID)
		}

		services = append(services, options.Services[i])
	}

	entries, err := b.buildServices(ctx, services)
	if err != nil {
		return nil, err
	}

	b.report.Request = &catalogClient.CreateApplicationRequest{
		CatalogID:  arch.ID,
		Name:       appName,
		Services:   entries,
		Version:    arch.Version,
		WorkerName: b.opts.WorkerName,
	}

	return &b.report, nil
}

// buildServices resolves the --model flags against the services, then builds an entry for each.
func (b *Builder) buildServices(ctx context.Context, services []catalogTypes.DeployOptionsService) ([]catalogClient.CreateApplicationService, error) {
	if err := b.resolveModels(ctx, services); err != nil {
		return nil, err
	}

	entries := make([]catalogClient.CreateApplicationService, 0, len(services))

	for i := range services {
		entry, err := b.serviceEntry(ctx, &services[i])
		if err != nil {
			return nil, fmt.Errorf("failed to build service '%s': %w", services[i].ID, err)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// modelComponentTypes lists the model component types the services use, in order of first use.
func modelComponentTypes(services []catalogTypes.DeployOptionsService) []string {
	var componentTypes []string

	for _, svc := range services {
		for _, c := range svc.Components {
			if c.Category == catalogConstants.ComponentCategoryModels && !slices.Contains(componentTypes, c.Type) {
				componentTypes = append(componentTypes, c.Type)
			}
		}
	}

	return componentTypes
}

// resolveModels checks the --model flags against the model component types of the services and resolves each into
// a binding. Nothing is requested from the API when a flag names a component type the template does not use.
func (b *Builder) resolveModels(ctx context.Context, services []catalogTypes.DeployOptionsService) error {
	componentTypes := modelComponentTypes(services)

	for _, req := range b.opts.Models {
		if slices.Contains(componentTypes, req.componentType) {
			continue
		}

		if len(componentTypes) == 0 {
			return fmt.Errorf("--model %s=...: the template has no model component types", req.componentType)
		}

		return fmt.Errorf("--model %s=...: the template does not use component_type %q; its model component types are: %s",
			req.componentType, req.componentType, strings.Join(componentTypes, ", "))
	}

	for _, req := range b.opts.Models {
		binding, err := b.models.resolve(ctx, req)
		if err != nil {
			return err
		}

		b.bindings[req.componentType] = binding

		if binding.hint != "" {
			b.report.Hints = append(b.report.Hints, binding.hint)
		}
	}

	return nil
}

// serviceEntry builds the entry of one service. A model component type supplied by a remote model goes to
// the connectors of the entry, and is not a component.
func (b *Builder) serviceEntry(ctx context.Context, svc *catalogTypes.DeployOptionsService) (catalogClient.CreateApplicationService, error) {
	components := make([]catalogClient.CreateApplicationComponent, 0, len(svc.Components))

	var connectors []catalogClient.CreateApplicationConnector

	for _, c := range svc.Components {
		if c.Category != catalogConstants.ComponentCategoryModels {
			comp, err := b.plainComponent(ctx, svc.ID, c)
			if err != nil {
				return catalogClient.CreateApplicationService{}, err
			}

			components = append(components, comp)

			continue
		}

		comp, conn, err := b.modelEntry(ctx, svc.ID, c)
		if err != nil {
			return catalogClient.CreateApplicationService{}, err
		}

		if conn != nil {
			connectors = append(connectors, *conn)
		} else {
			components = append(components, *comp)
		}
	}

	return catalogClient.CreateApplicationService{
		CatalogID:  svc.ID,
		Version:    svc.Version,
		Components: components,
		Connectors: connectors,
		Params:     extractServiceParams(svc.ID, svc.Components, b.opts.Params),
	}, nil
}

// modelEntry builds the entry of a model component type of a service: a component, or for a remote model
// the connector that supplies the component type instead.
func (b *Builder) modelEntry(ctx context.Context, serviceID string, c catalogTypes.DeployOptionsComponent) (*catalogClient.CreateApplicationComponent, *catalogClient.CreateApplicationConnector, error) {
	providerParams := extractComponentParamsForService(serviceID, c.Type, b.opts.Params)
	binding, bound := b.bindings[c.Type]

	// A registered model brings its own provider, so a provider choice contradicts it.
	if bound && binding.kind != bindNew {
		if len(providerParams) > 0 {
			return nil, nil, fmt.Errorf("a provider cannot be chosen for %q: it uses the registered model %q", c.Type, binding.registered.Name)
		}

		b.report.add(binding.describe())

		if binding.kind == bindConnector {
			return nil, &catalogClient.CreateApplicationConnector{ID: binding.registered.ID, Type: catalogConstants.ConnectorTypeModels}, nil
		}

		return &catalogClient.CreateApplicationComponent{ComponentType: c.Type, InstanceID: binding.registered.ID}, nil, nil
	}

	provider, userParams, err := b.pickModelProvider(c, providerParams, binding, bound)
	if err != nil {
		return nil, nil, err
	}

	if _, ok := userParams[modelParamKey]; ok {
		return nil, nil, fmt.Errorf("%s.%s.%s is not supported; choose the model with --model %s=<model>",
			c.Type, provider.ID, modelParamKey, c.Type)
	}

	model, source, err := b.modelFor(ctx, c.Type, provider, binding, bound)
	if err != nil {
		return nil, nil, err
	}

	b.report.add(fmt.Sprintf("%s: deploy new model %q with provider %q (%s)", c.Type, model, provider.ID, source))

	return &catalogClient.CreateApplicationComponent{
		ComponentType: c.Type,
		ProviderID:    provider.ID,
		Version:       provider.Version,
		Model:         model,
		Params:        toNestedParams(userParams),
	}, nil, nil
}

// pickModelProvider picks the provider a model component type is deployed through. When --model chose a
// model, only providers that serve it are candidates, and an explicit provider choice that does not
// is an error rather than being silently replaced.
func (b *Builder) pickModelProvider(
	c catalogTypes.DeployOptionsComponent,
	providerParams map[string]map[string]string,
	binding modelBinding,
	bound bool,
) (*catalogTypes.DeployOptionsProvider, map[string]string, error) {
	candidates := c

	if bound {
		candidates.Providers = slices.DeleteFunc(slices.Clone(c.Providers), func(p catalogTypes.DeployOptionsProvider) bool {
			return !offersLocally(binding.providers, p.ID)
		})

		for id := range providerParams {
			if !offersLocally(binding.providers, id) {
				return nil, nil, checkOfferedLocally(binding.model, binding.providers, id)
			}
		}

		if len(candidates.Providers) == 0 {
			return nil, nil, fmt.Errorf("model %q cannot be deployed for %q: none of the providers of the service serve it", binding.model, c.Type)
		}
	}

	return selectProvider(candidates, providerParams)
}

// modelFor returns the model to deploy for a component type with the provider, and where the choice came from.
func (b *Builder) modelFor(
	ctx context.Context,
	componentType string,
	provider *catalogTypes.DeployOptionsProvider,
	binding modelBinding,
	bound bool,
) (string, string, error) {
	if bound {
		return binding.model, "requested with --model", nil
	}

	key := componentTypeProvider{componentType: componentType, provider: provider.ID}
	if model, ok := b.chosen[key]; ok {
		return model, b.chosenSource(provider), nil
	}

	model, err := b.recommendedOrDefault(ctx, componentType, provider)
	if err != nil {
		return "", "", err
	}

	b.chosen[key] = model

	return model, b.chosenSource(provider), nil
}

// chosenSource says where a model chosen without --model came from.
func (b *Builder) chosenSource(provider *catalogTypes.DeployOptionsProvider) string {
	if len(provider.Recommendations) > 0 {
		return "recommended by the service"
	}

	return fmt.Sprintf("default of provider %q", provider.ID)
}

// recommendedOrDefault returns the primary model the service recommends for the provider, or else
// the default model of the provider.
func (b *Builder) recommendedOrDefault(ctx context.Context, componentType string, provider *catalogTypes.DeployOptionsProvider) (string, error) {
	if len(provider.Recommendations) > 0 {
		return provider.Recommendations[0].Model, nil
	}

	return b.models.defaultModel(ctx, componentType, provider.ID)
}

// plainComponent builds the entry of a component that is not a model. Its provider comes from
// --params or the defaults, and its params from the provider's schema defaults and --params.
func (b *Builder) plainComponent(ctx context.Context, serviceID string, c catalogTypes.DeployOptionsComponent) (catalogClient.CreateApplicationComponent, error) {
	providerParams := extractComponentParamsForService(serviceID, c.Type, b.opts.Params)

	provider, userParams, err := selectProvider(c, providerParams)
	if err != nil {
		return catalogClient.CreateApplicationComponent{}, err
	}

	params, err := b.withSchemaDefaults(ctx, c.Type, provider.ID, userParams)
	if err != nil {
		logger.Warningf("Failed to apply schema defaults for %s/%s: %v\n", c.Type, provider.ID, err)

		// Continue with user-provided params only.
		params = toNestedParams(userParams)
	}

	return catalogClient.CreateApplicationComponent{
		ComponentType: c.Type,
		ProviderID:    provider.ID,
		Version:       provider.Version,
		Params:        params,
	}, nil
}

// withSchemaDefaults fetches the provider's schema and returns its defaults overridden by the
// user's params.
func (b *Builder) withSchemaDefaults(ctx context.Context, componentType, providerID string, userParams map[string]string) (map[string]any, error) {
	schema, err := b.deploy.GetComponentProviderParams(ctx, componentType, providerID, string(b.opts.Runtime))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	result := make(map[string]any)
	maps.Copy(result, extractDefaultsFromSchema(schema))
	maps.Copy(result, toNestedParams(userParams))

	return result, nil
}

// selectProvider picks the provider of a component and returns it with the user's params for it.
func selectProvider(
	c catalogTypes.DeployOptionsComponent,
	providerParams map[string]map[string]string,
) (*catalogTypes.DeployOptionsProvider, map[string]string, error) {
	providerID, userParams, err := selectProviderFromDeployOptions(c, providerParams)
	if err != nil {
		return nil, nil, err
	}

	if providerID == "" {
		return nil, nil, fmt.Errorf("no provider found for component type '%s'", c.Type)
	}

	i := slices.IndexFunc(c.Providers, func(p catalogTypes.DeployOptionsProvider) bool { return p.ID == providerID })
	if i < 0 {
		return nil, nil, fmt.Errorf("provider '%s' not found in deploy options for component type '%s'", providerID, c.Type)
	}

	return &c.Providers[i], userParams, nil
}

// toNestedParams converts dot-notation user params (e.g. auth.password) into nested maps. It never
// returns nil, as the API takes params as an object. The 'provider' key is not a param.
func toNestedParams(userParams map[string]string) map[string]any {
	out := make(map[string]any, len(userParams))

	for k, v := range userParams {
		if k != "provider" {
			setNestedParam(out, k, v)
		}
	}

	return out
}

// add records a line of the selections, once.
func (r *Result) add(line string) {
	if !slices.Contains(r.Selections, line) {
		r.Selections = append(r.Selections, line)
	}
}
