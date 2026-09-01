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
// falls back to the provided embedded catalog when the user is not logged in.
// Any other client-init error (bad config, etc.) is returned to the caller.
//
// If the API is reachable but returns a non-connectivity error (e.g. 4xx/5xx),
// the error is propagated so the caller sees it rather than silently falling
// back to stale embedded data.
//
// embedded must not be nil; it is consulted lazily — only when an API call
// fails with a connectivity error, or when the user is not logged in.
func NewCatalogSource(ctx context.Context, embedded EmbeddedCatalog) (CatalogSource, error) {
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
		ApplicationClient: apiClient,
		embedded:          embedded,
	}, nil
}

// --------------------------------------------------------------------------
// API-first source
// --------------------------------------------------------------------------

// apiSource wraps ApplicationClient to satisfy CatalogSource.
// It embeds *ApplicationClient directly — all methods except ListComponents
// and the two renamed loaders are promoted automatically.
// ListComponents has no API endpoint and always reads from the embedded catalog.
// LoadArchitecture/LoadService map to the client's GetArchitectureDetails/GetServiceDetails.
type apiSource struct {
	*ApplicationClient
	embedded EmbeddedCatalog
}

func (s *apiSource) LoadArchitecture(ctx context.Context, id string) (*types.Architecture, error) {
	return s.GetArchitectureDetails(ctx, id)
}

func (s *apiSource) LoadService(ctx context.Context, id string) (*types.Service, error) {
	return s.GetServiceDetails(ctx, id)
}

func (s *apiSource) ListComponents(_ context.Context) ([]types.Component, error) {
	return listComponentsFromEmbedded(s.embedded)
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
	return listComponentsFromEmbedded(s.embedded)
}

func (s *embeddedOnlySource) LoadArchitecture(_ context.Context, id string) (*types.Architecture, error) {
	arch, err := s.embedded.LoadArchitecture(id)
	if err != nil {
		return nil, err
	}
	if arch == nil {
		return nil, fmt.Errorf("architecture '%s' not found", id)
	}

	return arch, nil
}

func (s *embeddedOnlySource) LoadService(_ context.Context, id string) (*types.Service, error) {
	svc, err := s.embedded.LoadService(id)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, fmt.Errorf("service '%s' not found", id)
	}

	return svc, nil
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

// listComponentsFromEmbedded is shared by apiSource and embeddedOnlySource —
// components have no API endpoint so both always read from the embedded catalog.
func listComponentsFromEmbedded(e EmbeddedCatalog) ([]types.Component, error) {
	comps, err := e.ListComponents()
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}

	return comps, nil
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
			ID:            s.ID,
			Name:          s.Name,
			Description:   s.Description,
			CertifiedBy:   s.CertifiedBy,
			Architectures: s.Architectures,
			Standalone:    s.Standalone,
			Dependencies:  s.Dependencies,
		}
	}

	return summaries, nil
}
