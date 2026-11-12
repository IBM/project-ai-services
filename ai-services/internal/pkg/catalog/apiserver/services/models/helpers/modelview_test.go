package helpers

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCatalog_QueryComponentTypes(t *testing.T) {
	tests := []struct {
		name    string
		catalog ModelCatalog
		want    []string
	}{
		{
			name:    "model types of the embedded catalog, sorted",
			catalog: newTestModelCatalog(t),
			want:    []string{"embedding", "llm", "reranker"},
		},
		{
			// A nil restriction would be read by the repository as "no restriction" and return
			// every component, so an empty catalog must yield an empty, non-nil slice.
			name:    "catalog without model types is empty, not nil",
			catalog: New(&catalog.CatalogProvider{}),
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.catalog.QueryComponentTypes()

			require.NotNil(t, got)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelCatalog_ProviderNames(t *testing.T) {
	names := newTestModelCatalog(t).ProviderNames()

	tests := []struct {
		name     string
		key      string
		wantName string // "" means only that the entry exists, with a name other than its id
	}{
		{name: "remote provider carries its display name", key: "remote/watsonx", wantName: "Watsonx"},
		{name: "local provider is listed", key: "local/vllm-spyre"},
		{name: "second local provider is listed", key: "local/vllm-cpu"},
		{name: "second remote provider is listed", key: "remote/hosted_vllm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := names[tt.key]

			require.True(t, ok, "%q is missing from %v", tt.key, names)
			assert.NotEmpty(t, got)

			if tt.wantName != "" {
				assert.Equal(t, tt.wantName, got)
			}
		})
	}

	t.Run("keys are scoped by deployment type", func(t *testing.T) {
		assert.NotContains(t, names, "remote/vllm-spyre", "a local provider is not offered as remote")
		assert.NotContains(t, names, "local/watsonx", "a remote provider is not offered as local")
	})

	t.Run("only model providers are listed", func(t *testing.T) {
		for key := range names {
			assert.NotContains(t, key, "vector_db")
		}
	})

	t.Run("empty catalog has no providers", func(t *testing.T) {
		got := New(&catalog.CatalogProvider{}).ProviderNames()

		require.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestProviderKey(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType catalogconstants.DeploymentType
		providerID     string
		want           string
	}{
		{"local", catalogconstants.DeploymentTypeLocal, "vllm-spyre", "local/vllm-spyre"},
		{"remote", catalogconstants.DeploymentTypeRemote, "watsonx", "remote/watsonx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, providerKey(tt.deploymentType, tt.providerID))
		})
	}
}

func TestModelDependencyType(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType catalogconstants.DeploymentType
		want           dbmodels.DependencyType
	}{
		{"local models are components", catalogconstants.DeploymentTypeLocal, dbmodels.DependencyTypeComponent},
		{"remote models are connectors", catalogconstants.DeploymentTypeRemote, dbmodels.DependencyTypeConnector},
		{"unknown deployment type is a connector", catalogconstants.DeploymentType("other"), dbmodels.DependencyTypeConnector},
		{"empty deployment type is a connector", catalogconstants.DeploymentType(""), dbmodels.DependencyTypeConnector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ModelDependencyType(dbrepo.ModelRow{DeploymentType: tt.deploymentType}))
		})
	}
}

func TestProviderInfo(t *testing.T) {
	providers := map[string]string{
		"local/vllm-spyre": "vLLM Spyre",
		"remote/watsonx":   "Watsonx",
	}

	tests := []struct {
		name           string
		deploymentType catalogconstants.DeploymentType
		provider       string
		want           types.ProviderInfo
	}{
		{
			name:           "known local provider",
			deploymentType: catalogconstants.DeploymentTypeLocal,
			provider:       "vllm-spyre",
			want:           types.ProviderInfo{ID: "vllm-spyre", Name: "vLLM Spyre"},
		},
		{
			name:           "known remote provider",
			deploymentType: catalogconstants.DeploymentTypeRemote,
			provider:       "watsonx",
			want:           types.ProviderInfo{ID: "watsonx", Name: "Watsonx"},
		},
		{
			name:           "provider removed from the catalog falls back to its id",
			deploymentType: catalogconstants.DeploymentTypeRemote,
			provider:       "retired",
			want:           types.ProviderInfo{ID: "retired", Name: "retired"},
		},
		{
			name:           "same id under another deployment type is not matched",
			deploymentType: catalogconstants.DeploymentTypeLocal,
			provider:       "watsonx",
			want:           types.ProviderInfo{ID: "watsonx", Name: "watsonx"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := dbrepo.ModelRow{DeploymentType: tt.deploymentType, Provider: tt.provider}

			assert.Equal(t, tt.want, providerInfo(row, providers))
		})
	}

	t.Run("nil provider map falls back to the id", func(t *testing.T) {
		row := dbrepo.ModelRow{DeploymentType: catalogconstants.DeploymentTypeLocal, Provider: "vllm-spyre"}

		assert.Equal(t, types.ProviderInfo{ID: "vllm-spyre", Name: "vllm-spyre"}, providerInfo(row, nil))
	})
}

func TestWorkerInfo(t *testing.T) {
	tests := []struct {
		name string
		row  dbrepo.ModelRow
		want *types.ApplicationWorker
	}{
		{
			name: "no worker name means no worker",
			row:  dbrepo.ModelRow{WorkerID: "w-1", WorkerRuntimeType: "podman"},
			want: nil,
		},
		{
			name: "remote model has no worker",
			row:  dbrepo.ModelRow{DeploymentType: catalogconstants.DeploymentTypeRemote},
			want: nil,
		},
		{
			name: "worker carries id, name and runtime type",
			row:  dbrepo.ModelRow{WorkerID: "w-1", WorkerName: "worker-1", WorkerRuntimeType: "podman"},
			want: &types.ApplicationWorker{ID: "w-1", Name: "worker-1", RuntimeType: "podman"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, workerInfo(tt.row))
		})
	}
}

func TestModelCatalog_ComponentTypeInfos(t *testing.T) {
	infos := newTestModelCatalog(t).ComponentTypeInfos()

	for _, id := range []string{"llm", "embedding", "reranker"} {
		got, ok := infos[id]

		require.True(t, ok, "%q is missing from %v", id, infos)
		assert.Equal(t, id, got.ID)
		assert.NotEmpty(t, got.Name)
		assert.NotEmpty(t, got.ShortName)
	}

	t.Run("empty catalog has no component types", func(t *testing.T) {
		got := New(&catalog.CatalogProvider{}).ComponentTypeInfos()

		require.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestComponentTypeInfo(t *testing.T) {
	infos := map[string]types.ComponentType{
		"llm": {ID: "llm", Name: "Large Language Model (LLM)", ShortName: "LLM"},
	}

	assert.Equal(t, infos["llm"], componentTypeInfo(dbrepo.ModelRow{ComponentType: "llm"}, infos))
	assert.Equal(t, types.ComponentType{ID: "retired"}, componentTypeInfo(dbrepo.ModelRow{ComponentType: "retired"}, infos),
		"a type removed from the catalog falls back to its id")
	assert.Equal(t, types.ComponentType{ID: "llm"}, componentTypeInfo(dbrepo.ModelRow{ComponentType: "llm"}, nil))
}

func TestToModelBase(t *testing.T) {
	id := uuid.New()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	providers := map[string]string{"remote/watsonx": "Watsonx", "local/vllm-spyre": "vLLM Spyre"}
	componentTypes := map[string]types.ComponentType{
		"llm": {ID: "llm", Name: "Large Language Model (LLM)", ShortName: "LLM"},
	}

	local := dbrepo.ModelRow{
		ID:                id,
		DeploymentType:    catalogconstants.DeploymentTypeLocal,
		Name:              "granite",
		ComponentType:     "llm",
		Provider:          "vllm-spyre",
		Model:             "ibm-granite/granite-3.3-8b-instruct",
		Params:            map[string]any{"max_tokens": 1024},
		Status:            "running",
		Message:           "ok",
		WorkerID:          "w-1",
		WorkerName:        "worker-1",
		WorkerRuntimeType: "podman",
		CreatedAt:         created,
		UpdatedAt:         updated,
	}
	remote := dbrepo.ModelRow{
		ID:             id,
		DeploymentType: catalogconstants.DeploymentTypeRemote,
		Name:           "wx",
		Provider:       "watsonx",
		Model:          "ibm/granite",
		Status:         "ready",
		CreatedAt:      created,
		UpdatedAt:      updated,
	}
	retired := remote
	retired.Provider = "retired"

	tests := []struct {
		name     string
		row      dbrepo.ModelRow
		services int
		check    func(t *testing.T, got apimodels.ModelBase)
	}{
		{
			name:     "local model maps every field",
			row:      local,
			services: 3,
			check: func(t *testing.T, got apimodels.ModelBase) {
				assert.Equal(t, id.String(), got.ID)
				assert.Equal(t, catalogconstants.DeploymentTypeLocal, got.DeploymentType)
				assert.Equal(t, "granite", got.Name)
				assert.Equal(t, types.ComponentType{ID: "llm", Name: "Large Language Model (LLM)", ShortName: "LLM"}, got.ComponentType)
				assert.Equal(t, types.ProviderInfo{ID: "vllm-spyre", Name: "vLLM Spyre"}, got.Provider)
				assert.Equal(t, &types.ApplicationWorker{ID: "w-1", Name: "worker-1", RuntimeType: "podman"}, got.Worker)
				assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", got.Model)
				assert.Equal(t, map[string]any{"max_tokens": 1024}, got.Params)
				assert.Equal(t, "running", got.Status)
				assert.Equal(t, "ok", got.Message)
				assert.Equal(t, 3, got.ConnectedServices)
				assert.Equal(t, created, got.CreatedAt)
				assert.Equal(t, updated, got.UpdatedAt)
			},
		},
		{
			name: "remote model has no worker",
			row:  remote,
			check: func(t *testing.T, got apimodels.ModelBase) {
				assert.Nil(t, got.Worker)
				assert.Equal(t, types.ProviderInfo{ID: "watsonx", Name: "Watsonx"}, got.Provider)
				assert.Zero(t, got.ConnectedServices)
				assert.Nil(t, got.Params)
			},
		},
		{
			name: "unknown provider falls back to its id",
			row:  retired,
			check: func(t *testing.T, got apimodels.ModelBase) {
				assert.Equal(t, types.ProviderInfo{ID: "retired", Name: "retired"}, got.Provider)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, ToModelBase(tt.row, providers, componentTypes, tt.services))
		})
	}
}
