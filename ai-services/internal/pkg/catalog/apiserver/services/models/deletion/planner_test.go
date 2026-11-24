package deletion

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorkers is a worker.Resolver that hands out a fixed runtime (or error) and records the
// worker it was asked for.
type fakeWorkers struct {
	worker.Resolver
	rt         runtime.Runtime
	err        error
	askedFor   string
	namespaces []string
}

func (f *fakeWorkers) NewRuntime(_ context.Context, name, namespace string) (runtime.Runtime, error) {
	f.askedFor = name
	f.namespaces = append(f.namespaces, namespace)

	return f.rt, f.err
}

func newTestCatalogProvider(t *testing.T) *catalog.CatalogProvider {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	return provider
}

func modelMetadata(model, liteLLMModelID string) map[string]any {
	md := map[string]any{}
	if model != "" {
		md[catalogconstants.MetadataKeyModel] = model
	}
	if liteLLMModelID != "" {
		md[catalogconstants.MetadataKeyLiteLLM] = map[string]any{
			catalogconstants.MetadataKeyLiteLLMModelID: liteLLMModelID,
		}
	}

	return md
}

func TestLiteLLMModelID(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{name: "saved id", metadata: modelMetadata("", "litellm-1"), want: "litellm-1"},
		{name: "nil metadata", metadata: nil, want: ""},
		{name: "no litellm section", metadata: map[string]any{}, want: ""},
		{name: "litellm section of the wrong type", metadata: map[string]any{catalogconstants.MetadataKeyLiteLLM: "x"}, want: ""},
		{name: "litellm section without an id", metadata: map[string]any{catalogconstants.MetadataKeyLiteLLM: map[string]any{}}, want: ""},
		{
			name: "id of the wrong type",
			metadata: map[string]any{
				catalogconstants.MetadataKeyLiteLLM: map[string]any{catalogconstants.MetadataKeyLiteLLMModelID: 7},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, liteLLMModelID(tt.metadata))
		})
	}
}

func TestPlanRemote(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name string
		conn *dbmodels.Connector
		want litellmRouteWant
	}{
		{
			name: "fully registered model",
			conn: &dbmodels.Connector{ID: id, Provider: "watsonx", Metadata: modelMetadata("ibm/granite", "litellm-1")},
			want: litellmRouteWant{modelID: "litellm-1", keyAlias: "ibm/granite-" + id.String(), tag: id.String(), provider: "watsonx"},
		},
		{
			name: "model that never reached the gateway",
			conn: &dbmodels.Connector{ID: id, Provider: "hosted_vllm", Metadata: modelMetadata("ibm/granite", "")},
			want: litellmRouteWant{modelID: "", keyAlias: "ibm/granite-" + id.String(), tag: id.String(), provider: "hosted_vllm"},
		},
		{
			name: "no metadata: no model id, and an alias that matches no key",
			conn: &dbmodels.Connector{ID: id, Provider: "hosted_vllm"},
			want: litellmRouteWant{keyAlias: "-" + id.String(), tag: id.String(), provider: "hosted_vllm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := (&Planner{}).PlanRemote(tt.conn)

			assert.Equal(t, tt.conn.ID, plan.ConnectorID)
			assert.Equal(t, tt.want.modelID, plan.Route.LiteLLMModelID)
			assert.Equal(t, tt.want.keyAlias, plan.Route.KeyAlias, "the public key is aliased by model name and id")
			assert.Equal(t, tt.want.tag, plan.Route.Tag)
			assert.Equal(t, tt.want.provider, plan.Route.Provider)
		})
	}
}

type litellmRouteWant struct {
	modelID, keyAlias, tag, provider string
}

func TestPlanLocal(t *testing.T) {
	id := uuid.New()
	comp := func(provider string) *dbmodels.Component {
		return &dbmodels.Component{
			ID:       id,
			Type:     "llm",
			Provider: provider,
			Metadata: modelMetadata("ibm/granite", "litellm-1"),
		}
	}

	tests := []struct {
		name         string
		comp         *dbmodels.Component
		workerName   string
		workers      *fakeWorkers
		wantWorker   string
		wantProvider string
		wantCode     int
	}{
		{
			name:         "control-plane model resolves to the local worker",
			comp:         comp("vllm-cpu"),
			workerName:   "",
			workers:      &fakeWorkers{rt: mock.New(runtimeTypes.RuntimeTypePodman)},
			wantWorker:   workerconstants.LocalWorkerName,
			wantProvider: "hosted_vllm",
		},
		{
			name:         "remote worker keeps its name",
			comp:         comp("vllm-cpu"),
			workerName:   "worker-1",
			workers:      &fakeWorkers{rt: mock.New(runtimeTypes.RuntimeTypePodman)},
			wantWorker:   "worker-1",
			wantProvider: "hosted_vllm",
		},
		{
			name:         "a provider the catalog no longer lists does not block the delete",
			comp:         comp("removed-provider"),
			workers:      &fakeWorkers{rt: mock.New(runtimeTypes.RuntimeTypePodman)},
			wantWorker:   workerconstants.LocalWorkerName,
			wantProvider: "",
		},
		{
			name:       "unreachable worker is a 422 before anything is touched",
			comp:       comp("vllm-cpu"),
			workerName: "worker-1",
			workers:    &fakeWorkers{err: errors.New("worker not connected")},
			wantWorker: "worker-1",
			wantCode:   http.StatusUnprocessableEntity,
		},
		{
			name:       "models are unsupported on openshift",
			comp:       comp("vllm-cpu"),
			workers:    &fakeWorkers{rt: mock.New(runtimeTypes.RuntimeTypeOpenShift)},
			wantWorker: workerconstants.LocalWorkerName,
			wantCode:   http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner := NewPlanner(newTestCatalogProvider(t), tt.workers)

			plan, err := planner.PlanLocal(context.Background(), tt.comp, tt.workerName)

			assert.Equal(t, tt.wantWorker, tt.workers.askedFor)

			if tt.wantCode != 0 {
				var verr *validators.ValidationError
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, tt.wantCode, verr.Code)
				assert.Nil(t, plan)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, id, plan.ComponentID)
			assert.Equal(t, tt.workers.rt, plan.Runtime)
			assert.Equal(t, "litellm-1", plan.Route.LiteLLMModelID)
			assert.Equal(t, "ibm/granite-"+id.String(), plan.Route.KeyAlias)
			assert.Equal(t, id.String(), plan.Route.Tag)
			assert.Equal(t, tt.wantProvider, plan.Route.Provider)
		})
	}
}
