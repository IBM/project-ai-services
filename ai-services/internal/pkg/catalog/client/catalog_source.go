package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// isConnectivityError returns true for any error that is not an HTTPError.
// HTTPError is produced by our client for non-2xx responses — those are
// server-side errors that should be propagated. Everything else (dial failures,
// timeouts, TLS errors) triggers the embedded catalog fallback.
func isConnectivityError(err error) bool {
	var httpErr *HTTPError

	return !errors.As(err, &httpErr)
}

// CatalogSource is the read interface the CLI uses for listing and inspecting
// catalog templates. List operations return summary types that contain all
// fields needed for CLI display. Single-item loads return full types.
type CatalogSource interface {
	// ListArchitectures returns summaries of all available architecture templates.
	ListArchitectures(ctx context.Context) ([]types.ArchitectureSummary, error)
	// ListServices returns summaries of all available service templates, including
	// their dependency references so the CLI can display required components.
	ListServices(ctx context.Context) ([]types.ServiceSummary, error)
	// ListComponents returns all available component templates.
	// This always reads from the embedded catalog — no API endpoint exists.
	ListComponents(ctx context.Context) ([]types.Component, error)
	// LoadArchitecture returns the full details of a single architecture.
	LoadArchitecture(ctx context.Context, id string) (*types.Architecture, error)
	// LoadService returns the full details of a single service.
	LoadService(ctx context.Context, id string) (*types.Service, error)
	// GetServiceParams returns the JSON schema for a service's parameters.
	// runtimeType selects the runtime subdirectory (e.g. "podman" or "openshift").
	GetServiceParams(ctx context.Context, serviceID, runtimeType string) (map[string]any, error)
	// GetComponentProviderParams returns the JSON schema for a component provider's parameters.
	// runtimeType selects the runtime subdirectory (e.g. "podman" or "openshift").
	GetComponentProviderParams(ctx context.Context, componentType, providerID, runtimeType string) (map[string]any, error)
}

// NewCatalogSource builds a CatalogSource that tries the catalog API first and
// falls back to the local embedded catalog when the user is not logged in.
// Any other client-init error (bad config, etc.) is returned to the caller.
//
// If the API is reachable but returns a non-connectivity error (e.g. 4xx/5xx),
// the error is propagated so the caller sees it rather than silently falling
// back to stale embedded data.
func NewCatalogSource(ctx context.Context) (CatalogSource, error) {
	embedded, err := newEmbeddedCatalog()
	if err != nil {
		return nil, err
	}

	apiClient, err := NewApplicationClient(ctx)
	if err != nil {
		if !errors.Is(err, config.ErrNotLoggedIn) {
			return nil, fmt.Errorf("failed to connect to catalog API: %w", err)
		}

		// No active session — fall back to embedded-only local provider.
		logger.Warningln("Not logged in to catalog server, falling back to local embedded catalog (custom bundle items will not be included)")

		return &embeddedOnlySource{embedded: embedded}, nil
	}

	return &apiSource{
		api:      apiClient,
		embedded: embedded,
	}, nil
}

// newEmbeddedCatalog creates a local CatalogProvider with no bundle DB.
// It is the embedded fallback used when the catalog API is unreachable or
// the user is not logged in.
func newEmbeddedCatalog() (EmbeddedCatalog, error) {
	provider, err := catalog.NewCatalogProvider(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded catalog provider: %w", err)
	}

	return provider, nil
}

// --------------------------------------------------------------------------
// API-first source with embedded fallback
// --------------------------------------------------------------------------

// apiSource tries the catalog API for every call. On a connectivity failure
// (anything that is not an HTTPError) it falls back to the embedded catalog.
// HTTPErrors (4xx/5xx) are propagated — the server is reachable, so the caller
// should see the error rather than silently getting stale embedded data.
type apiSource struct {
	api      *ApplicationClient
	embedded EmbeddedCatalog
}

func (s *apiSource) ListArchitectures(ctx context.Context) ([]types.ArchitectureSummary, error) {
	summaries, err := s.api.ListArchitectures(ctx)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API ListArchitectures unreachable, falling back to embedded: %v", err)

		return toArchitectureSummaries(s.embedded.ListArchitectures())
	}

	return summaries, err
}

func (s *apiSource) ListServices(ctx context.Context) ([]types.ServiceSummary, error) {
	summaries, err := s.api.ListServices(ctx)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API ListServices unreachable, falling back to embedded: %v", err)

		return toServiceSummaries(s.embedded.ListServices())
	}

	return summaries, err
}

func (s *apiSource) ListComponents(_ context.Context) ([]types.Component, error) {
	return listComponentsFromEmbedded(s.embedded)
}

func (s *apiSource) LoadArchitecture(ctx context.Context, id string) (*types.Architecture, error) {
	arch, err := s.api.GetArchitectureDetails(ctx, id)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API GetArchitectureDetails unreachable, falling back to embedded: %v", err)

		return loadArchitectureFromEmbedded(s.embedded, id)
	}

	return arch, err
}

func (s *apiSource) LoadService(ctx context.Context, id string) (*types.Service, error) {
	svc, err := s.api.GetServiceDetails(ctx, id)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API GetServiceDetails unreachable, falling back to embedded: %v", err)

		return loadServiceFromEmbedded(s.embedded, id)
	}

	return svc, err
}

func (s *apiSource) GetServiceParams(ctx context.Context, serviceID, runtimeType string) (map[string]any, error) {
	schema, err := s.api.GetServiceParams(ctx, serviceID, runtimeType)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API GetServiceParams unreachable, falling back to embedded: %v", err)

		scopedCatalog, scopeErr := s.embedded.WithRuntime(runtimeType)
		if scopeErr != nil {
			return nil, scopeErr
		}

		return scopedCatalog.GetServiceParams(ctx, serviceID)
	}

	return schema, err
}

func (s *apiSource) GetComponentProviderParams(ctx context.Context, componentType, providerID, runtimeType string) (map[string]any, error) {
	schema, err := s.api.GetComponentProviderParams(ctx, componentType, providerID, runtimeType)
	if err != nil && isConnectivityError(err) {
		logger.DebugfCtx(ctx, "API GetComponentProviderParams unreachable, falling back to embedded: %v", err)

		scopedCatalog, scopeErr := s.embedded.WithRuntime(runtimeType)
		if scopeErr != nil {
			return nil, scopeErr
		}

		return scopedCatalog.GetComponentProviderParams(ctx, componentType, providerID)
	}

	return schema, err
}

// --------------------------------------------------------------------------
// Embedded-only source (used when the API client cannot be initialised)
// --------------------------------------------------------------------------

// EmbeddedCatalog is the subset of catalog.CatalogProvider that CatalogSource
// uses as its fallback. Accepting an interface rather than the concrete
// *catalog.CatalogProvider keeps the dependency minimal and eases testing.
type EmbeddedCatalog interface {
	ListArchitectures() ([]types.Architecture, error)
	ListServices() ([]types.Service, error)
	ListComponents() ([]types.Component, error)
	LoadArchitecture(id string) (*types.Architecture, error)
	LoadService(id string) (*types.Service, error)
	WithRuntime(runtimeType string) (*catalog.CatalogProvider, error)
}

type embeddedOnlySource struct {
	embedded EmbeddedCatalog
}

func (s *embeddedOnlySource) ListArchitectures(_ context.Context) ([]types.ArchitectureSummary, error) {
	return toArchitectureSummaries(s.embedded.ListArchitectures())
}

func (s *embeddedOnlySource) ListServices(_ context.Context) ([]types.ServiceSummary, error) {
	return toServiceSummaries(s.embedded.ListServices())
}

func (s *embeddedOnlySource) ListComponents(_ context.Context) ([]types.Component, error) {
	return listComponentsFromEmbedded(s.embedded)
}

func (s *embeddedOnlySource) LoadArchitecture(_ context.Context, id string) (*types.Architecture, error) {
	return loadArchitectureFromEmbedded(s.embedded, id)
}

func (s *embeddedOnlySource) LoadService(_ context.Context, id string) (*types.Service, error) {
	return loadServiceFromEmbedded(s.embedded, id)
}

func (s *embeddedOnlySource) GetServiceParams(ctx context.Context, serviceID, runtimeType string) (map[string]any, error) {
	scopedCatalog, err := s.embedded.WithRuntime(runtimeType)
	if err != nil {
		return nil, err
	}

	return scopedCatalog.GetServiceParams(ctx, serviceID)
}

func (s *embeddedOnlySource) GetComponentProviderParams(ctx context.Context, componentType, providerID, runtimeType string) (map[string]any, error) {
	scopedCatalog, err := s.embedded.WithRuntime(runtimeType)
	if err != nil {
		return nil, err
	}

	return scopedCatalog.GetComponentProviderParams(ctx, componentType, providerID)
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// listComponentsFromEmbedded is shared by apiSource and embeddedOnlySource —
// components have no API endpoint so both always read from the embedded catalog.
func listComponentsFromEmbedded(e EmbeddedCatalog) ([]types.Component, error) {
	comps, err := e.ListComponents()
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}

	return comps, nil
}

// loadArchitectureFromEmbedded converts a nil result into a proper error so
// callers never receive (nil, nil).
func loadArchitectureFromEmbedded(e EmbeddedCatalog, id string) (*types.Architecture, error) {
	arch, err := e.LoadArchitecture(id)
	if err != nil {
		return nil, err
	}
	if arch == nil {
		return nil, fmt.Errorf("architecture '%s' not found", id)
	}

	return arch, nil
}

// loadServiceFromEmbedded converts a nil result into a proper error so callers
// never receive (nil, nil).
func loadServiceFromEmbedded(e EmbeddedCatalog, id string) (*types.Service, error) {
	svc, err := e.LoadService(id)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, fmt.Errorf("service '%s' not found", id)
	}

	return svc, nil
}

// toArchitectureSummaries converts []Architecture to []ArchitectureSummary.
func toArchitectureSummaries(archs []types.Architecture, err error) ([]types.ArchitectureSummary, error) {
	if err != nil {
		return nil, err
	}

	summaries := make([]types.ArchitectureSummary, len(archs))
	for i := range archs {
		summaries[i] = catalog.ToArchitectureSummary(&archs[i])
	}

	return summaries, nil
}

// toServiceSummaries converts []Service to []ServiceSummary.
func toServiceSummaries(svcs []types.Service, err error) ([]types.ServiceSummary, error) {
	if err != nil {
		return nil, err
	}

	summaries := make([]types.ServiceSummary, len(svcs))
	for i := range svcs {
		summaries[i] = catalog.ToServiceSummary(&svcs[i])
	}

	return summaries, nil
}
