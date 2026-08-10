package connectors

import (
	"fmt"
	"io/fs"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"go.yaml.in/yaml/v3"
)

// ProviderRegistry holds metadata for all registered connector providers, keyed by
// connector_type then provider ID. It is populated once at startup by loadMetadata.
type ProviderRegistry struct {
	// metadata maps connector_type → provider_id → ConnectorProvider (raw asset metadata)
	metadata map[string]map[string]*types.ConnectorProvider
}

// NewProviderRegistry creates a ProviderRegistry, loading metadata.yaml files from the
// embedded ConnectorsFS.
func NewProviderRegistry() (*ProviderRegistry, error) {
	r := &ProviderRegistry{
		metadata: make(map[string]map[string]*types.ConnectorProvider),
	}

	if err := r.loadMetadata(); err != nil {
		return nil, err
	}

	return r, nil
}

// loadMetadata walks assets/connectors/*/*/metadata.yaml, parses each file into a
// ConnectorProvider, and stores it in the metadata map.
func (r *ProviderRegistry) loadMetadata() error {
	err := fs.WalkDir(&assets.ConnectorsFS, "connectors", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "metadata.yaml" {
			return nil
		}

		data, readErr := assets.ConnectorsFS.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", path, readErr)
		}

		var cp types.ConnectorProvider
		if unmarshalErr := yaml.Unmarshal(data, &cp); unmarshalErr != nil {
			return fmt.Errorf("failed to parse %s: %w", path, unmarshalErr)
		}

		if cp.ConnectorType == "" || cp.ID == "" {
			return nil // skip malformed entries
		}

		if r.metadata[cp.ConnectorType] == nil {
			r.metadata[cp.ConnectorType] = make(map[string]*types.ConnectorProvider)
		}
		cpCopy := cp
		r.metadata[cp.ConnectorType][cp.ID] = &cpCopy

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to load connector metadata: %w", err)
	}

	return nil
}

// GetProviderMetadata returns the raw ConnectorProvider metadata for the given connector
// type and provider ID. Returns an error if the combination is not found.
func (r *ProviderRegistry) GetProviderMetadata(connectorType, providerID string) (*types.ConnectorProvider, error) {
	byType, ok := r.metadata[connectorType]
	if !ok {
		return nil, fmt.Errorf("connector type %q not found", connectorType)
	}
	cp, ok := byType[providerID]
	if !ok {
		return nil, fmt.Errorf("provider %q not found for connector type %q", providerID, connectorType)
	}

	return cp, nil
}

// ListProviders returns all ConnectorProvider metadata entries for the given connector type.
// Returns an error when the type is not registered.
func (r *ProviderRegistry) ListProviders(connectorType string) ([]*types.ConnectorProvider, error) {
	byType, ok := r.metadata[connectorType]
	if !ok {
		return nil, fmt.Errorf("connector type %q not found", connectorType)
	}
	result := make([]*types.ConnectorProvider, 0, len(byType))
	for _, cp := range byType {
		result = append(result, cp)
	}

	return result, nil
}

// ListAllProviders returns every ConnectorProvider across all registered connector types.
func (r *ProviderRegistry) ListAllProviders() []*types.ConnectorProvider {
	result := make([]*types.ConnectorProvider, 0)
	for _, byType := range r.metadata {
		for _, cp := range byType {
			result = append(result, cp)
		}
	}

	return result
}

// Made with Bob
