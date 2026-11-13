package catalog

import (
	"testing"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/profiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListArchitectures(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	start := time.Now()
	architectures, err := provider.ListArchitectures()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("ListArchitectures() error = %v", err)

		return
	}

	t.Logf("ListArchitectures: Found %d architectures in %v", len(architectures), elapsed)
	for _, arch := range architectures {
		t.Logf("  - %s: %s", arch.ID, arch.Name)
	}
}

func TestLoadArchitecture(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	tests := []struct {
		name           string
		architectureID string
		wantErr        bool
	}{
		{
			name:           "Load existing architecture",
			architectureID: "rag",
			wantErr:        false,
		},
		{
			name:           "Load non-existent architecture",
			architectureID: "non-existent",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			arch, err := provider.LoadArchitecture(tt.architectureID)
			elapsed := time.Since(start)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadArchitecture() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				t.Logf("LoadArchitecture(%s): Loaded '%s' in %v", tt.architectureID, arch.Name, elapsed)
			} else {
				t.Logf("LoadArchitecture(%s): Expected error received in %v", tt.architectureID, elapsed)
			}
		})
	}
}

func TestListServices(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	start := time.Now()
	services, err := provider.ListServices()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("ListServices() error = %v", err)

		return
	}

	t.Logf("ListServices: Found %d services in %v", len(services), elapsed)
	for _, svc := range services {
		t.Logf("  - %s: %s", svc.ID, svc.Name)
	}
}

func TestLoadService(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	tests := []struct {
		name      string
		serviceID string
		wantErr   bool
	}{
		{
			name:      "Load regular service",
			serviceID: "chat",
			wantErr:   false,
		},
		{
			name:      "Load non-existent service",
			serviceID: "non-existent",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			service, err := provider.LoadService(tt.serviceID)
			elapsed := time.Since(start)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadService() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				t.Logf("LoadService(%s): Loaded '%s' in %v", tt.serviceID, service.Name, elapsed)
			} else {
				t.Logf("LoadService(%s): Expected error received in %v", tt.serviceID, elapsed)
			}
		})
	}
}

func TestListComponents(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	start := time.Now()
	components, err := provider.ListComponents()
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("ListComponents() error = %v", err)

		return
	}

	t.Logf("ListComponents: Found %d components in %v", len(components), elapsed)
	for _, comp := range components {
		t.Logf("  - %s: %s (%s)", comp.ID, comp.Name, comp.ComponentType)
	}
}

func TestLoadComponent(t *testing.T) {
	provider, err := NewCatalogProvider(nil)
	if err != nil {
		t.Fatalf("Failed to create catalog provider: %v", err)
	}

	tests := []struct {
		name          string
		componentType string
		componentID   string
		wantErr       bool
	}{
		{
			name:          "Load opensearch component",
			componentType: "vector_store",
			componentID:   "opensearch",
			wantErr:       false,
		},
		{
			name:          "Load vllm-cpu reranker component",
			componentType: "reranker",
			componentID:   "vllm-cpu",
			wantErr:       false,
		},
		{
			name:          "Load vllm-cpu embedding component",
			componentType: "embedding",
			componentID:   "vllm-cpu",
			wantErr:       false,
		},
		{
			name:          "Load non-existent component",
			componentType: "unknown",
			componentID:   "non-existent-component",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			component, err := provider.LoadComponent(tt.componentType, tt.componentID)
			elapsed := time.Since(start)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadComponent() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				t.Logf("LoadComponent(%s/%s): Loaded '%s' (%s) in %v", tt.componentType, tt.componentID, component.Name, component.ComponentType, elapsed)
			} else {
				t.Logf("LoadComponent(%s/%s): Expected error received in %v", tt.componentType, tt.componentID, elapsed)
			}
		})
	}
}

func TestLoadComponentValuesForModel(t *testing.T) {
	const granite33 = "ibm-granite/granite-3.3-8b-instruct"

	p := newPodmanProvider(t)

	t.Run("default profile of the model is merged into the values", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("llm", "vllm-spyre", granite33, "", nil)
		require.NoError(t, err)

		assert.Equal(t, granite33, v["model"])
		assert.Equal(t, "standard", v["profile"])
		assert.Equal(t, 32768, v["maxModelLen"])
		assert.Equal(t, "2Gi", v["shmSize"])
		assert.Equal(t, map[string]int{"ibm.com/spyre_pf": 4}, v["resources"].(map[string]any)["accelerators"])
	})

	t.Run("requested profile overrides the default", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("llm", "vllm-spyre", granite33, "compact-4k", nil)
		require.NoError(t, err)

		assert.Equal(t, "compact-4k", v["profile"])
		assert.Equal(t, 4096, v["maxModelLen"])
		assert.Equal(t, map[string]int{"ibm.com/spyre_pf": 1}, v["resources"].(map[string]any)["accelerators"])
	})

	t.Run("cpu provider has no accelerators and carries maxNumBatchedTokens", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("llm", "vllm-cpu", granite33, "", nil)
		require.NoError(t, err)

		assert.Equal(t, 8096, v["maxNumBatchedTokens"])
		assert.NotContains(t, v["resources"], "accelerators")
	})

	t.Run("no model falls back to the provider default profile", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("llm", "vllm-spyre", "", "", nil)
		require.NoError(t, err)

		assert.Equal(t, 32768, v["maxModelLen"])
		assert.Empty(t, v["model"])
	})

	t.Run("profile values win over argParams, other argParams are kept", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("llm", "vllm-spyre", granite33, "", map[string]string{"maxModelLen": "1", "apiKey": "k"})
		require.NoError(t, err)

		assert.Equal(t, 32768, v["maxModelLen"])
		assert.Equal(t, "k", v["apiKey"])
	})

	t.Run("provider without profiles loads plain values", func(t *testing.T) {
		v, err := p.LoadComponentValuesForModel("vector_store", "opensearch", "", "", nil)
		require.NoError(t, err)

		assert.NotContains(t, v, "profile")
		assert.NotContains(t, v, "maxModelLen")
	})

	t.Run("errors", func(t *testing.T) {
		_, err := p.LoadComponentValuesForModel("llm", "vllm-spyre", "no/such-model", "", nil)
		assert.ErrorIs(t, err, ErrModelNotSupported, "unknown model")

		_, err = p.LoadComponentValuesForModel("vector_store", "opensearch", granite33, "", nil)
		assert.ErrorIs(t, err, ErrModelNotSupported, "provider without models.yaml")

		// vllm-cpu lists only "standard" for this model.
		_, err = p.LoadComponentValuesForModel("llm", "vllm-cpu", granite33, "compact-4k", nil)
		assert.ErrorIs(t, err, profiles.ErrNotSupported, "profile not listed for the model")

		_, err = p.LoadComponentValuesForModel("llm", "no-such-provider", granite33, "", nil)
		assert.Error(t, err, "unknown provider")
	})
}

// Made with Bob
