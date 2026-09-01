package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

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
	GetServiceParams(ctx context.Context, serviceID string) (map[string]any, error)
	// GetComponentProviderParams returns the JSON schema for a component provider's parameters.
	GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error)
}

// NewCatalogSource builds a CatalogSource that tries the catalog API first and
// falls back to the provided embedded catalog when a network-level error occurs.
//
// If the API client cannot be initialised because the user is not logged in
// (config.ErrNotLoggedIn), no fallback is attempted — a clear error is returned
// so the user knows they need to run `ai-services catalog login`. Only genuine
// connectivity failures (dial errors, timeouts) trigger the fallback.
//
// embedded must not be nil; pass a *catalog.CatalogProvider(nil) from the
// calling layer. It is consulted lazily — only when the API call fails.
func NewCatalogSource(ctx context.Context, embedded EmbeddedCatalog) (CatalogSource, error) {
	apiClient, err := NewApplicationClient(ctx)
	if err != nil {
		if errors.Is(err, config.ErrNotLoggedIn) {
			// Auth config is missing — do not silently fall back; propagate.
			return nil, err
		}
		// Any other client-init error (e.g. bad config file) — fall back and warn.
		logger.WarningfCtx(ctx, "catalog API client unavailable, using embedded catalog: %v", err)

		return &embeddedOnlySource{embedded: embedded}, nil
	}

	return &apiWithFallback{
		api:      apiClient,
		embedded: embedded,
	}, nil
}

// --------------------------------------------------------------------------
// API-first with embedded fallback
// --------------------------------------------------------------------------

type apiWithFallback struct {
	api      *ApplicationClient
	embedded EmbeddedCatalog
}

func (s *apiWithFallback) ListArchitectures(ctx context.Context) ([]types.ArchitectureSummary, error) {
	summaries, err := s.api.ListArchitectures(ctx)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API ListArchitectures failed, falling back to embedded: %v", err)

			return toArchitectureSummaries(s.embedded.ListArchitectures())
		}

		return nil, err
	}

	return summaries, nil
}

func (s *apiWithFallback) ListServices(ctx context.Context) ([]types.ServiceSummary, error) {
	summaries, err := s.api.ListServices(ctx)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API ListServices failed, falling back to embedded: %v", err)

			return toServiceSummaries(s.embedded.ListServices())
		}

		return nil, err
	}

	return summaries, nil
}

func (s *apiWithFallback) ListComponents(ctx context.Context) ([]types.Component, error) {
	// No API endpoint for listing components — always use embedded catalog.
	comps, err := s.embedded.ListComponents()
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}

	return comps, nil
}

func (s *apiWithFallback) LoadArchitecture(ctx context.Context, id string) (*types.Architecture, error) {
	arch, err := s.api.GetArchitectureDetails(ctx, id)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API GetArchitectureDetails failed, falling back to embedded: %v", err)

			return loadArchitectureFromEmbedded(s.embedded, id)
		}

		return nil, err
	}

	return arch, nil
}

func (s *apiWithFallback) LoadService(ctx context.Context, id string) (*types.Service, error) {
	svc, err := s.api.GetServiceDetails(ctx, id)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API GetServiceDetails failed, falling back to embedded: %v", err)

			return loadServiceFromEmbedded(s.embedded, id)
		}

		return nil, err
	}

	return svc, nil
}

func (s *apiWithFallback) GetServiceParams(ctx context.Context, serviceID string) (map[string]any, error) {
	schema, err := s.api.GetServiceParams(ctx, serviceID)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API GetServiceParams failed, falling back to embedded: %v", err)

			return s.embedded.GetServiceParams(ctx, serviceID)
		}

		return nil, err
	}

	return schema, nil
}

func (s *apiWithFallback) GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error) {
	schema, err := s.api.GetComponentProviderParams(ctx, componentType, providerID)
	if err != nil {
		if isConnectivityError(err) {
			logger.DebugfCtx(ctx, "API GetComponentProviderParams failed, falling back to embedded: %v", err)

			return s.embedded.GetComponentProviderParams(ctx, componentType, providerID)
		}

		return nil, err
	}

	return schema, nil
}

// --------------------------------------------------------------------------
// Embedded-only source (used when the API client cannot be initialised)
// --------------------------------------------------------------------------

// EmbeddedCatalog is the subset of catalog.CatalogProvider that CatalogSource
// uses as its fallback. Accepting an interface keeps this package free of a
// direct import of the catalog package (which itself imports catalog/client
// types), avoiding a tight coupling.
type EmbeddedCatalog interface {
	ListArchitectures() ([]types.Architecture, error)
	ListServices() ([]types.Service, error)
	ListComponents() ([]types.Component, error)
	LoadArchitecture(id string) (*types.Architecture, error)
	LoadService(id string) (*types.Service, error)
	GetServiceParams(ctx context.Context, serviceID string) (map[string]any, error)
	GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error)
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
	comps, err := s.embedded.ListComponents()
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}

	return comps, nil
}

func (s *embeddedOnlySource) LoadArchitecture(_ context.Context, id string) (*types.Architecture, error) {
	return loadArchitectureFromEmbedded(s.embedded, id)
}

func (s *embeddedOnlySource) LoadService(_ context.Context, id string) (*types.Service, error) {
	return loadServiceFromEmbedded(s.embedded, id)
}

func (s *embeddedOnlySource) GetServiceParams(ctx context.Context, serviceID string) (map[string]any, error) {
	return s.embedded.GetServiceParams(ctx, serviceID)
}

func (s *embeddedOnlySource) GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error) {
	return s.embedded.GetComponentProviderParams(ctx, componentType, providerID)
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// isConnectivityError returns true for errors that indicate the API server is
// unreachable (network dial errors, timeouts, connection refused). API-level
// errors such as 4xx/5xx are not connectivity errors and should be propagated.
func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	// HTTPError is produced by our client for non-2xx responses — that is a
	// server-side error, not a connectivity failure.
	var httpErr *HTTPError

	return !errors.As(err, &httpErr)
}

// loadArchitectureFromEmbedded wraps embedded LoadArchitecture, converting a
// nil result into a proper error so callers never receive (nil, nil).
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

// loadServiceFromEmbedded wraps embedded LoadService, converting a nil result
// into a proper error so callers never receive (nil, nil).
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

// toArchitectureSummaries converts []Architecture to []ArchitectureSummary,
// extracting only the fields needed for CLI list display.
func toArchitectureSummaries(archs []types.Architecture, err error) ([]types.ArchitectureSummary, error) {
	if err != nil {
		return nil, err
	}

	summaries := make([]types.ArchitectureSummary, len(archs))
	for i, a := range archs {
		svcIDs := make([]string, len(a.Services))
		for j, s := range a.Services {
			svcIDs[j] = s.ID
		}
		summaries[i] = types.ArchitectureSummary{
			ID:          a.ID,
			Name:        a.Name,
			Description: a.Description,
			CertifiedBy: a.CertifiedBy,
			Services:    svcIDs,
		}
	}

	return summaries, nil
}

// toServiceSummaries converts []Service to []ServiceSummary, including
// Dependencies so the CLI can display required components.
func toServiceSummaries(svcs []types.Service, err error) ([]types.ServiceSummary, error) {
	if err != nil {
		return nil, err
	}

	summaries := make([]types.ServiceSummary, len(svcs))
	for i, s := range svcs {
		summaries[i] = types.ServiceSummary{
			ID:           s.ID,
			Name:         s.Name,
			Description:  s.Description,
			CertifiedBy:  s.CertifiedBy,
			Standalone:   s.Standalone,
			Dependencies: s.Dependencies,
		}
	}

	return summaries, nil
}
