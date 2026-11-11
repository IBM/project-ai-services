package catalog

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPodmanProvider returns a catalog provider scoped to the podman runtime.
func newPodmanProvider(t *testing.T) *CatalogProvider {
	t.Helper()
	base, err := NewCatalogProvider(nil)
	require.NoError(t, err)
	p, err := base.WithRuntime("podman")
	require.NoError(t, err)

	return p
}

func TestGetModelDeployOptions(t *testing.T) {
	const granite33 = "ibm-granite/granite-3.3-8b-instruct"

	tests := []struct {
		name          string
		modelID       string
		providerID    string
		wantErr       error
		wantName      string
		wantType      string
		wantProviders []string // provider IDs, in response order
		check         func(t *testing.T, opts *types.ModelDeployOptions)
	}{
		{
			name:    "unknown model",
			modelID: "no/such-model",
			wantErr: ErrModelNotFound,
		},
		{
			// Served by vllm-spyre (3 profiles), vllm-cpu (1 profile) and the hosted_vllm connector.
			// Order: local before remote, default provider (vllm-spyre) first.
			name:          "local and remote providers",
			modelID:       granite33,
			wantName:      "Granite 3.3 8B Instruct",
			wantType:      "llm",
			wantProviders: []string{"vllm-spyre", "vllm-cpu", "hosted_vllm"},
			check: func(t *testing.T, opts *types.ModelDeployOptions) {
				spyre := opts.Providers[0]
				assert.Equal(t, constants.DeploymentTypeLocal, spyre.DeploymentType)
				assert.True(t, spyre.Default)
				assert.Equal(t, "/api/v1/components/llm/providers/vllm-spyre/params?runtime=podman", spyre.Schema)
				require.Len(t, spyre.Profiles, 3)
				assert.Equal(t, "standard", spyre.Profiles[0].ID)
				assert.True(t, spyre.Profiles[0].Default)
				assert.Equal(t, 214748364800, spyre.Profiles[0].Resources.Memory)
				assert.Equal(t, 4, spyre.Profiles[0].Resources.Accelerators["ibm.com/spyre_pf"])
				assert.Equal(t, "compact-4k", spyre.Profiles[2].ID)
				assert.False(t, spyre.Profiles[2].Default)
				assert.Equal(t, 1, spyre.Profiles[2].Resources.Accelerators["ibm.com/spyre_pf"])

				remote := opts.Providers[2]
				assert.Equal(t, constants.DeploymentTypeRemote, remote.DeploymentType)
				assert.Empty(t, remote.Profiles)
				assert.Equal(t, "/api/v1/connectors/models/providers/hosted_vllm/params", remote.Schema)
			},
		},
		{
			name:          "provider filter",
			modelID:       granite33,
			providerID:    "vllm-cpu",
			wantName:      "Granite 3.3 8B Instruct",
			wantType:      "llm",
			wantProviders: []string{"vllm-cpu"},
		},
		{
			// Known model, filter matches no provider: empty list, not a 404.
			name:          "provider filter with no match",
			modelID:       granite33,
			providerID:    "watsonx",
			wantName:      "Granite 3.3 8B Instruct",
			wantType:      "llm",
			wantProviders: []string{},
		},
		{
			// llm/watsonx is a legacy per-app component without profiles, so only the connector is returned.
			name:          "remote-only model",
			modelID:       "ibm/granite-4-h-small",
			wantName:      "Granite 4 H Small",
			wantType:      "llm",
			wantProviders: []string{"watsonx"},
		},
		{
			name:          "embedding model",
			modelID:       "ibm-granite/granite-embedding-278m-multilingual",
			wantName:      "Granite Embedding 278M Multilingual",
			wantType:      "embedding",
			wantProviders: []string{"vllm-cpu", "hosted_vllm"},
			check: func(t *testing.T, opts *types.ModelDeployOptions) {
				require.Len(t, opts.Providers[0].Profiles, 1)
				assert.Equal(t, "standard", opts.Providers[0].Profiles[0].ID)
			},
		},
	}

	provider := newPodmanProvider(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := provider.GetModelDeployOptions(context.Background(), tt.modelID, tt.providerID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.modelID, opts.ID)
			assert.Equal(t, tt.wantName, opts.Name)
			assert.Equal(t, tt.wantType, opts.ComponentType)

			gotProviders := make([]string, 0, len(opts.Providers))
			for _, pr := range opts.Providers {
				gotProviders = append(gotProviders, pr.ID)
			}
			assert.Equal(t, tt.wantProviders, gotProviders)

			if tt.check != nil {
				tt.check(t, opts)
			}
		})
	}
}
