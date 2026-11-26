package deployment

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPodman  = "podman"
	localWorker = "Local"
)

// fakeModelManager is a ModelManager that answers the plan phase from canned values and records
// what it was asked. The phases the planner does not use are left to the embedded nil interface,
// so a test that reaches one fails loudly.
type fakeModelManager struct {
	ModelManager

	instance    *dbmodels.Component
	instanceErr error
	connector   *dbmodels.Connector
	connectors  map[uuid.UUID]*dbmodels.Connector
	connErr     error
	planErr     error
	checkErr    error

	gotInstanceID, gotType, gotCreatedBy string
	gotPlanReq                           *apimodels.CreateModelRequest
	gotCheckPlans                        []*modeltypes.LocalPlan
}

func (f *fakeModelManager) GetConnectedModel(_ context.Context, id uuid.UUID, createdBy string) (*dbmodels.Connector, error) {
	f.gotCreatedBy = createdBy

	if conn, ok := f.connectors[id]; ok {
		return conn, f.connErr
	}

	return f.connector, f.connErr
}

func (f *fakeModelManager) GetRunningInstance(_ context.Context, id uuid.UUID, componentType, createdBy string) (*dbmodels.Component, error) {
	f.gotInstanceID, f.gotType, f.gotCreatedBy = id.String(), componentType, createdBy

	return f.instance, f.instanceErr
}

func (f *fakeModelManager) CheckLocalModels(_ context.Context, _ string, plans []*modeltypes.LocalPlan) error {
	f.gotCheckPlans = plans

	return f.checkErr
}

func (f *fakeModelManager) PlanLocalModel(_ context.Context, req apimodels.CreateModelRequest) (*modeltypes.LocalPlan, error) {
	f.gotPlanReq = &req
	if f.planErr != nil {
		return nil, f.planErr
	}

	return &modeltypes.LocalPlan{ComponentID: uuid.New(), Req: req}, nil
}

// fakeWorkers is a worker.Resolver that knows the given workers and their database IDs.
type fakeWorkers struct {
	worker.Resolver

	ids map[string]uuid.UUID
}

func (f fakeWorkers) ID(name string) (uuid.UUID, bool) {
	id, ok := f.ids[name]

	return id, ok
}

func newTestPlanner(t *testing.T, models ModelManager, workers worker.Resolver) *DeploymentPlanner {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	planner, err := NewDeploymentPlanner(provider, workers, models).WithRuntime(testPodman)
	require.NoError(t, err)

	return planner
}

func newTestPlan(createdBy string) *DeploymentPlan {
	return &DeploymentPlan{
		CreatedBy:   createdBy,
		WorkerName:  localWorker,
		RuntimeType: testPodman,
		Components:  map[string]*ComponentPlan{},
	}
}

func assertValidationCode(t *testing.T, err error, code int) {
	t.Helper()

	var verr *validators.ValidationError

	require.ErrorAs(t, err, &verr)
	assert.Equal(t, code, verr.Code)
}

func runningModel() *dbmodels.Component {
	return &dbmodels.Component{ID: uuid.New(), Type: "llm", Name: "m-1a2b3c4d"}
}

func TestResolveComponent(t *testing.T) {
	workerID := uuid.New()
	workers := fakeWorkers{ids: map[string]uuid.UUID{localWorker: workerID}}
	instanceID := uuid.New()

	running := func() *dbmodels.Component {
		return &dbmodels.Component{
			ID: instanceID, Type: "llm", Provider: "vllm-cpu", Version: "1.0", WorkerID: &workerID,
			Metadata: map[string]any{catalogconstants.MetadataKeyModel: "ibm/granite"},
		}
	}

	t.Run("a component without instance_id is returned as is, without the model service", func(t *testing.T) {
		planner := newTestPlanner(t, nil, workers)
		comp := apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch"}

		rc, err := planner.resolveComponent(t.Context(), comp, newTestPlan("alice"))

		require.NoError(t, err)
		assert.Equal(t, comp, rc.req)
		assert.Nil(t, rc.instance)
	})

	t.Run("an instance completes provider, version and model", func(t *testing.T) {
		fake := &fakeModelManager{instance: running()}
		planner := newTestPlanner(t, fake, workers)

		rc, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, newTestPlan("alice"))

		require.NoError(t, err)
		assert.Equal(t, instanceID.String(), fake.gotInstanceID)
		assert.Equal(t, "llm", fake.gotType)
		assert.Equal(t, "alice", fake.gotCreatedBy, "the instance must belong to the application's creator")
		assert.Equal(t, "vllm-cpu", rc.req.ProviderID)
		assert.Equal(t, "1.0", rc.req.Version)
		assert.Equal(t, "ibm/granite", rc.req.Model)
		assert.Same(t, fake.instance, rc.instance)
	})

	t.Run("instance_id only applies to a model component", func(t *testing.T) {
		fake := &fakeModelManager{instance: running()}
		planner := newTestPlanner(t, fake, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "vector_store", InstanceID: instanceID.String()}, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
		assert.ErrorContains(t, err, "not a model component")
		assert.Empty(t, fake.gotInstanceID, "the model service is not asked about an instance of a non-model type")
	})

	t.Run("instance_id needs the model service", func(t *testing.T) {
		planner := newTestPlanner(t, nil, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})

	t.Run("an instance is looked up through the model service on every runtime", func(t *testing.T) {
		unsupported := validators.BadRequest("models are unsupported on openshift runtime")
		fake := &fakeModelManager{instanceErr: unsupported}
		planner := newTestPlanner(t, fake, workers)
		plan := newTestPlan("alice")
		plan.RuntimeType = "openshift"

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, plan)

		assert.Same(t, unsupported, err, "the model service decides which runtimes are supported")
		assert.Equal(t, instanceID.String(), fake.gotInstanceID)
	})

	t.Run("instance_id must be a UUID", func(t *testing.T) {
		fake := &fakeModelManager{instance: running()}
		planner := newTestPlanner(t, fake, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: "nope"}, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
		assert.Empty(t, fake.gotInstanceID, "the model service is not asked about an invalid id")
	})

	t.Run("errors of the model service are returned unchanged", func(t *testing.T) {
		forbidden := validators.Forbidden("user does not own model instance")
		planner := newTestPlanner(t, &fakeModelManager{instanceErr: forbidden}, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, newTestPlan("alice"))

		assert.Same(t, forbidden, err)
	})

	t.Run("an instance on another worker is rejected", func(t *testing.T) {
		other := uuid.New()
		inst := running()
		inst.WorkerID = &other
		planner := newTestPlanner(t, &fakeModelManager{instance: inst}, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
		assert.ErrorContains(t, err, "own worker")
	})

	t.Run("an instance without a worker is rejected", func(t *testing.T) {
		inst := running()
		inst.WorkerID = nil
		planner := newTestPlanner(t, &fakeModelManager{instance: inst}, workers)

		_, err := planner.resolveComponent(t.Context(),
			apimodels.Component{ComponentType: "llm", InstanceID: instanceID.String()}, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})
}

func TestPlanModel(t *testing.T) {
	llm := apimodels.Component{
		ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1.0", Model: "ibm/granite",
		Params: map[string]any{"max_len": 4096},
	}

	t.Run("a new model is planned by the model service", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)
		plan := newTestPlan("alice")
		comp := &ComponentPlan{ComponentType: "llm"}

		require.NoError(t, planner.planModel(t.Context(), resolvedComponent{req: llm}, "compact-4k", comp, plan))

		require.NotNil(t, fake.gotPlanReq)
		assert.Equal(t, "compact-4k", fake.gotPlanReq.Profile, "the resolved profile reaches the model service")
		assert.Equal(t, catalogconstants.DeploymentTypeLocal, fake.gotPlanReq.DeploymentType)
		assert.Equal(t, "llm", fake.gotPlanReq.ComponentType)
		assert.Equal(t, "vllm-cpu", fake.gotPlanReq.ProviderID)
		assert.Equal(t, "ibm/granite", fake.gotPlanReq.Model)
		assert.Equal(t, localWorker, fake.gotPlanReq.WorkerName)
		assert.Equal(t, "alice", fake.gotPlanReq.CreatedBy)
		assert.Equal(t, map[string]any{"max_len": 4096}, fake.gotPlanReq.Params, "the model is not a param")

		require.NotNil(t, comp.Model)
		assert.Equal(t, comp.Model.ComponentID, comp.DatabaseID, "the component id is the model plan's")
		assert.True(t, comp.IsModel())
		assert.False(t, comp.PreDeployed)
	})

	t.Run("an existing instance is bound without planning", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)
		inst := runningModel()
		comp := &ComponentPlan{ComponentType: "llm"}

		require.NoError(t, planner.planModel(t.Context(), resolvedComponent{req: llm, instance: inst}, "", comp, newTestPlan("alice")))

		assert.Nil(t, fake.gotPlanReq, "nothing is planned for an existing instance")
		assert.Nil(t, comp.Model)
		assert.True(t, comp.PreDeployed)
		assert.True(t, comp.IsModel())
		assert.Equal(t, inst.ID, comp.DatabaseID)
		assert.Empty(t, comp.Endpoints, "the services reach it through the gateway, not at its server")
	})

	t.Run("a component that is not a model is left alone", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)
		comp := &ComponentPlan{ComponentType: "vector_store"}

		err := planner.planModel(t.Context(),
			resolvedComponent{req: apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch"}}, "", comp, newTestPlan("alice"))

		require.NoError(t, err)
		assert.Nil(t, fake.gotPlanReq)
		assert.False(t, comp.IsModel())
	})

	t.Run("without the model service a model is a generic component", func(t *testing.T) {
		planner := newTestPlanner(t, nil, nil)
		comp := &ComponentPlan{ComponentType: "llm"}

		require.NoError(t, planner.planModel(t.Context(), resolvedComponent{req: llm}, "", comp, newTestPlan("alice")))
		assert.False(t, comp.IsModel())
	})

	t.Run("a model is planned through the model service on every runtime", func(t *testing.T) {
		unsupported := validators.BadRequest("models are unsupported on openshift runtime")
		fake := &fakeModelManager{planErr: unsupported}
		planner := newTestPlanner(t, fake, nil)
		plan := newTestPlan("alice")
		plan.RuntimeType = "openshift"
		comp := &ComponentPlan{ComponentType: "llm"}

		err := planner.planModel(t.Context(), resolvedComponent{req: llm}, "", comp, plan)

		assert.Same(t, unsupported, err, "the application is refused with the model service's error")
		require.NotNil(t, fake.gotPlanReq, "the model service is asked, whatever the runtime")
		assert.False(t, comp.IsModel())
	})

	t.Run("a model service error is returned unchanged", func(t *testing.T) {
		invalid := validators.BadRequest("invalid model")
		planner := newTestPlanner(t, &fakeModelManager{planErr: invalid}, nil)

		err := planner.planModel(t.Context(), resolvedComponent{req: llm}, "", &ComponentPlan{ComponentType: "llm"}, newTestPlan("alice"))

		assert.Same(t, invalid, err)
	})

	t.Run("other errors are returned too", func(t *testing.T) {
		boom := errors.New("boom")
		planner := newTestPlanner(t, &fakeModelManager{planErr: boom}, nil)

		err := planner.planModel(t.Context(), resolvedComponent{req: llm}, "", &ComponentPlan{ComponentType: "llm"}, newTestPlan("alice"))

		require.ErrorIs(t, err, boom)
	})
}

func TestProcessComponent(t *testing.T) {
	llm := apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1.0", Model: "ibm/granite"}

	t.Run("a model component carries the model plan and the model as a param", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")

		hash, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "chat", plan)

		require.NoError(t, err)
		comp := plan.Components[hash]
		require.NotNil(t, comp)
		assert.NotNil(t, comp.Model)
		assert.Equal(t, comp.Model.ComponentID, comp.DatabaseID)
		assert.Equal(t, "ibm/granite", comp.Params["model"])
		assert.Equal(t, []string{"chat"}, comp.UsedByServices)
	})

	t.Run("the same model used by two services is planned once", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)
		plan := newTestPlan("alice")

		first, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "chat", plan)
		require.NoError(t, err)

		fake.gotPlanReq = nil

		second, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "digitize", plan)
		require.NoError(t, err)

		assert.Equal(t, first, second)
		assert.Nil(t, fake.gotPlanReq, "the model is not planned a second time")
		assert.Len(t, plan.Components, 1)
		assert.Equal(t, []string{"chat", "digitize"}, plan.Components[first].UsedByServices)
	})

	t.Run("the order of the params does not matter", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)
		plan := newTestPlan("alice")
		a, b := llm, llm
		a.Params = map[string]any{"x": 1, "y": 2}
		b.Params = map[string]any{"y": 2, "x": 1}

		first, err := planner.processComponent(t.Context(), resolvedComponent{req: a}, "chat", plan)
		require.NoError(t, err)

		second, err := planner.processComponent(t.Context(), resolvedComponent{req: b}, "digitize", plan)
		require.NoError(t, err)

		assert.Equal(t, first, second)
		assert.Len(t, plan.ModelComponents(), 1, "one model is deployed for both services")
	})

	t.Run("the same model with other params is a different component", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")
		other := llm
		other.Params = map[string]any{"max_len": 8192}

		a, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "chat", plan)
		require.NoError(t, err)

		b, err := planner.processComponent(t.Context(), resolvedComponent{req: other}, "chat", plan)
		require.NoError(t, err)

		assert.NotEqual(t, a, b)
	})

	t.Run("different models are different components", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")
		other := llm
		other.Model = "ibm/other"

		a, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "chat", plan)
		require.NoError(t, err)

		b, err := planner.processComponent(t.Context(), resolvedComponent{req: other}, "chat", plan)
		require.NoError(t, err)

		assert.NotEqual(t, a, b)
		assert.Len(t, plan.ModelComponents(), 2)
	})

	t.Run("two instances of the same provider are different components", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")
		var hashes []string

		for range 2 {
			inst := runningModel()
			req := apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1.0", Model: "ibm/granite", InstanceID: inst.ID.String()}

			hash, err := planner.processComponent(t.Context(), resolvedComponent{req: req, instance: inst}, "chat", plan)
			require.NoError(t, err)

			hashes = append(hashes, hash)
		}

		assert.NotEqual(t, hashes[0], hashes[1])
		assert.Empty(t, plan.ModelComponents(), "existing instances are not deployed")
	})
}

func TestResolveConnectors(t *testing.T) {
	connID := uuid.New()
	modelConnector := func(role string) *dbmodels.Connector {
		return &dbmodels.Connector{
			ID: connID, Type: catalogconstants.ConnectorTypeModels, Provider: "watsonx",
			Metadata: map[string]any{
				catalogconstants.MetadataKeyComponentType: role,
				catalogconstants.MetadataKeyModel:         "ibm/granite-4-h-small",
			},
		}
	}
	ref := apimodels.ConnectorRef{ID: connID.String(), Type: catalogconstants.ConnectorTypeModels}
	// chat depends on vector_store, embedding and llm.
	chat := apimodels.Service{CatalogID: "chat", Connectors: []apimodels.ConnectorRef{ref}}

	t.Run("a model connector becomes the model component of its role", func(t *testing.T) {
		fake := &fakeModelManager{connector: modelConnector("llm")}
		planner := newTestPlanner(t, fake, nil)

		got, err := planner.resolveConnectors(t.Context(), chat, newTestPlan("alice"))

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "llm", got[0].req.ComponentType)
		assert.Equal(t, "watsonx", got[0].req.ProviderID)
		assert.Equal(t, "ibm/granite-4-h-small", got[0].req.Model)
		assert.Equal(t, connID.String(), got[0].req.InstanceID)
		assert.Equal(t, "alice", fake.gotCreatedBy)
	})

	t.Run("other connector types are left alone", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		svc := apimodels.Service{CatalogID: "chat", Connectors: []apimodels.ConnectorRef{{ID: uuid.NewString(), Type: "datasource"}}}

		got, err := planner.resolveConnectors(t.Context(), svc, newTestPlan("alice"))

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("the connector lookup error is returned", func(t *testing.T) {
		forbidden := validators.Forbidden("user does not own model connector")
		planner := newTestPlanner(t, &fakeModelManager{connErr: forbidden}, nil)

		_, err := planner.resolveConnectors(t.Context(), chat, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusForbidden)
	})

	t.Run("a role the service does not use is a 400", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{connector: modelConnector("reranker")}, nil)

		_, err := planner.resolveConnectors(t.Context(), chat, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})

	t.Run("a role also set as a component is a 400", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{connector: modelConnector("llm")}, nil)
		svc := chat
		svc.Components = []apimodels.Component{{ComponentType: "llm", InstanceID: uuid.NewString()}}

		_, err := planner.resolveConnectors(t.Context(), svc, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})

	t.Run("two connectors of the same role are a 400", func(t *testing.T) {
		other := modelConnector("llm")
		other.ID = uuid.New()
		fake := &fakeModelManager{connectors: map[uuid.UUID]*dbmodels.Connector{connID: modelConnector("llm"), other.ID: other}}
		planner := newTestPlanner(t, fake, nil)
		svc := chat
		svc.Connectors = append(slices.Clone(svc.Connectors), apimodels.ConnectorRef{ID: other.ID.String(), Type: catalogconstants.ConnectorTypeModels})

		_, err := planner.resolveConnectors(t.Context(), svc, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})

	t.Run("a connector and a component of other roles are valid together", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{connector: modelConnector("llm")}, nil)
		svc := chat
		svc.Components = []apimodels.Component{{ComponentType: "embedding", ProviderID: "vllm-cpu", Version: "1.0"}}

		got, err := planner.resolveConnectors(t.Context(), svc, newTestPlan("alice"))

		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("without model management a model connector is a 400", func(t *testing.T) {
		planner := newTestPlanner(t, nil, nil)

		_, err := planner.resolveConnectors(t.Context(), chat, newTestPlan("alice"))

		assertValidationCode(t, err, http.StatusBadRequest)
	})
}

func TestProcessComponentRemote(t *testing.T) {
	conn := &dbmodels.Connector{ID: uuid.New(), Type: catalogconstants.ConnectorTypeModels, Provider: "watsonx"}
	req := apimodels.Component{ComponentType: "llm", ProviderID: "watsonx", Model: "ibm/granite-4-h-small", InstanceID: conn.ID.String()}

	t.Run("a remote model is recorded as a remote pre-deployed model of the connector", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")

		hash, err := planner.processComponent(t.Context(), resolvedComponent{req: req, connector: conn}, "chat", plan)

		require.NoError(t, err)
		comp := plan.Components[hash]
		assert.True(t, comp.PreDeployed)
		assert.True(t, comp.Remote)
		assert.True(t, comp.IsModel())
		assert.Nil(t, comp.Model)
		assert.Equal(t, conn.ID, comp.DatabaseID)
		assert.Equal(t, "ibm/granite-4-h-small", comp.Params["model"])
		assert.Empty(t, comp.CatalogPath, "nothing is deployed for a remote model")
		assert.Empty(t, plan.ModelComponents(), "the model service deploys nothing")
	})

	t.Run("a remote model used by two services is planned once", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")

		first, err := planner.processComponent(t.Context(), resolvedComponent{req: req, connector: conn}, "chat", plan)
		require.NoError(t, err)

		second, err := planner.processComponent(t.Context(), resolvedComponent{req: req, connector: conn}, "digitize", plan)
		require.NoError(t, err)

		assert.Equal(t, first, second)
		assert.Equal(t, []string{"chat", "digitize"}, plan.Components[first].UsedByServices)
	})
}

func TestRemoteModelValues(t *testing.T) {
	connector := func(params map[string]any) *dbmodels.Connector {
		return &dbmodels.Connector{Metadata: map[string]any{catalogconstants.MetadataKeyParams: params}}
	}

	t.Run("the context length and batch size come from the DB params of the connector", func(t *testing.T) {
		llm := remoteModelValues(
			apimodels.Component{ComponentType: "llm", Model: "ibm/granite"},
			connector(map[string]any{"maxModelLen": 131072, "maxBatchSize": 64, "api_base": "http://h"}))

		assert.Equal(t, map[string]any{"model": "ibm/granite", "maxModelLen": 131072, "maxBatchSize": 64}, llm)
	})

	t.Run("numbers read back from the DB as JSON floats are whole numbers", func(t *testing.T) {
		llm := remoteModelValues(
			apimodels.Component{ComponentType: "llm", Model: "ibm/granite"},
			connector(map[string]any{"maxModelLen": float64(131072), "maxBatchSize": float64(64)}))

		assert.Equal(t, map[string]any{"model": "ibm/granite", "maxModelLen": 131072, "maxBatchSize": 64}, llm)
	})

	t.Run("only the params the connector holds are set", func(t *testing.T) {
		embedding := remoteModelValues(
			apimodels.Component{ComponentType: "embedding", Model: "bge"},
			connector(map[string]any{"maxModelLen": 512}))

		assert.Equal(t, map[string]any{"model": "bge", "maxModelLen": 512}, embedding)
	})

	t.Run("a connector without params leaves the model only", func(t *testing.T) {
		comp := apimodels.Component{ComponentType: "llm", Model: "ibm/granite"}
		want := map[string]any{"model": "ibm/granite"}

		assert.Equal(t, want, remoteModelValues(comp, &dbmodels.Connector{}))
		assert.Equal(t, want, remoteModelValues(comp, nil))
	})
}

func TestCheckModelSources(t *testing.T) {
	plan := &DeploymentPlan{Components: map[string]*ComponentPlan{
		"instance-llm":  {ComponentType: "llm", PreDeployed: true},
		"remote-llm":    {ComponentType: "llm", PreDeployed: true, Remote: true},
		"remote-embed":  {ComponentType: "embedding", PreDeployed: true, Remote: true},
		"deployed-llm":  {ComponentType: "llm"},
		"instance-llm2": {ComponentType: "llm", PreDeployed: true},
	}}
	check := func(refs ...string) error {
		return checkModelSources("chat", &ServicePlan{ComponentRefs: refs}, plan)
	}

	t.Run("an instance and a connector of the same type are a 400, in either order", func(t *testing.T) {
		assertValidationCode(t, check("instance-llm", "remote-llm"), http.StatusBadRequest)
		assertValidationCode(t, check("remote-llm", "instance-llm"), http.StatusBadRequest)
	})

	t.Run("an instance and a connector of different types are valid", func(t *testing.T) {
		require.NoError(t, check("instance-llm", "remote-embed"))
	})

	t.Run("a single source per type is valid", func(t *testing.T) {
		require.NoError(t, check("remote-llm"))
		require.NoError(t, check("instance-llm", "instance-llm2"))
		require.NoError(t, check("deployed-llm", "remote-embed"))
	})
}
func TestCheckModels(t *testing.T) {
	deployed := &modeltypes.LocalPlan{ComponentID: uuid.New()}
	other := &modeltypes.LocalPlan{ComponentID: uuid.New()}
	plan := &DeploymentPlan{
		WorkerName: localWorker,
		Components: map[string]*ComponentPlan{
			"b": {Hash: "b", Model: other},
			"a": {Hash: "a", Model: deployed},
			"x": {Hash: "x", ComponentType: "vector_store"},
			"p": {Hash: "p", PreDeployed: true},
		},
	}

	t.Run("hands the model service every model to deploy, in hash order, and nothing else", func(t *testing.T) {
		fake := &fakeModelManager{}

		require.NoError(t, newTestPlanner(t, fake, fakeWorkers{}).checkModels(t.Context(), plan))
		assert.Equal(t, []*modeltypes.LocalPlan{deployed, other}, fake.gotCheckPlans)
	})

	t.Run("a worker that cannot host the models fails the plan with the model service's error", func(t *testing.T) {
		unprocessable := validators.Unprocessable("worker %q cannot host these models", localWorker)
		fake := &fakeModelManager{checkErr: unprocessable}

		err := newTestPlanner(t, fake, fakeWorkers{}).checkModels(t.Context(), plan)

		assertValidationCode(t, err, http.StatusUnprocessableEntity)
	})

	t.Run("without model management there is nothing to check", func(t *testing.T) {
		require.NoError(t, newTestPlanner(t, nil, fakeWorkers{}).checkModels(t.Context(), plan))
	})
}

func TestModelProfile(t *testing.T) {
	const (
		granite = "ibm-granite/granite-3.3-8b-instruct"
		chat    = "chat"
	)

	spyreLLM := func(model string) apimodels.Component {
		return apimodels.Component{ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0", Model: model}
	}

	planner := newTestPlanner(t, &fakeModelManager{}, nil)

	t.Run("a recommended profile wins over the model's default", func(t *testing.T) {
		profile, err := planner.modelProfile("extract", resolvedComponent{req: spyreLLM(granite)})

		require.NoError(t, err)
		assert.Equal(t, "compact-4k", profile)
	})

	t.Run("without a recommendation the model's default profile applies", func(t *testing.T) {
		// chat declares no recommendations.
		profile, err := planner.modelProfile(chat, resolvedComponent{req: spyreLLM(granite)})

		require.NoError(t, err)
		assert.Equal(t, "standard", profile)
	})

	t.Run("a recommendation for another provider does not apply", func(t *testing.T) {
		comp := apimodels.Component{ComponentType: "llm", ProviderID: "vllm-cpu", Version: "1.0", Model: granite}

		profile, err := planner.modelProfile("extract", resolvedComponent{req: comp})

		require.NoError(t, err)
		assert.Equal(t, "standard", profile)
	})

	t.Run("a provider with a single profile resolves to it", func(t *testing.T) {
		comp := apimodels.Component{
			ComponentType: "embedding", ProviderID: "vllm-cpu", Version: "1.0",
			Model: "ibm-granite/granite-embedding-278m-multilingual",
		}

		profile, err := planner.modelProfile(chat, resolvedComponent{req: comp})

		require.NoError(t, err)
		assert.Equal(t, "standard", profile)
	})

	t.Run("a model that is not listed is left to the model service to reject", func(t *testing.T) {
		profile, err := planner.modelProfile(chat, resolvedComponent{req: spyreLLM("ibm/unknown")})

		require.NoError(t, err)
		assert.Empty(t, profile)
	})

	t.Run("an existing instance runs with the profile stored on its row", func(t *testing.T) {
		inst := runningModel()
		inst.Metadata = map[string]any{catalogconstants.MetadataKeyProfile: "balanced"}

		// chat recommends nothing: the instance's own profile still wins over the model's default.
		profile, err := planner.modelProfile(chat, resolvedComponent{req: spyreLLM(granite), instance: inst})

		require.NoError(t, err)
		assert.Equal(t, "balanced", profile)
	})

	t.Run("a profile already set on the component is kept", func(t *testing.T) {
		comp := spyreLLM(granite)
		comp.Profile = "balanced"

		// extract recommends compact-4k: what is already set is not resolved again.
		profile, err := planner.modelProfile("extract", resolvedComponent{req: comp})

		require.NoError(t, err)
		assert.Equal(t, "balanced", profile)
	})

	t.Run("an existing instance without a stored profile, a remote model and a non-model have no profile", func(t *testing.T) {
		for name, rc := range map[string]resolvedComponent{
			"instance":  {req: spyreLLM(granite), instance: runningModel()},
			"connector": {req: spyreLLM(granite), connector: &dbmodels.Connector{ID: uuid.New()}},
			"no model":  {req: apimodels.Component{ComponentType: "llm", ProviderID: "vllm-spyre"}},
			"non-model": {req: apimodels.Component{ComponentType: "vector_store", ProviderID: "opensearch", Model: granite}},
		} {
			profile, err := planner.modelProfile(chat, rc)

			require.NoError(t, err, name)
			assert.Empty(t, profile, name)
		}
	})

	t.Run("an unknown service is an error", func(t *testing.T) {
		_, err := planner.modelProfile("no-such-service", resolvedComponent{req: spyreLLM(granite)})

		require.Error(t, err)
	})
}

func TestProcessComponentProfile(t *testing.T) {
	llm := apimodels.Component{
		ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0", Model: "ibm-granite/granite-3.3-8b-instruct",
	}

	t.Run("the service's recommended profile is planned", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)

		_, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "extract", newTestPlan("alice"))

		require.NoError(t, err)
		require.NotNil(t, fake.gotPlanReq)
		assert.Equal(t, "compact-4k", fake.gotPlanReq.Profile)
	})

	t.Run("the model's default profile is planned when the service recommends none", func(t *testing.T) {
		fake := &fakeModelManager{}
		planner := newTestPlanner(t, fake, nil)

		_, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "chat", newTestPlan("alice"))

		require.NoError(t, err)
		require.NotNil(t, fake.gotPlanReq)
		assert.Equal(t, "standard", fake.gotPlanReq.Profile)
	})

	t.Run("services that need another profile of the same model get their own deployment", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")

		a, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "digitize", plan)
		require.NoError(t, err)

		b, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "extract", plan)
		require.NoError(t, err)

		assert.NotEqual(t, a, b)
		assert.Len(t, plan.ModelComponents(), 2)
	})

	t.Run("services that agree on the profile share one deployment", func(t *testing.T) {
		planner := newTestPlanner(t, &fakeModelManager{}, nil)
		plan := newTestPlan("alice")

		a, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "digitize", plan)
		require.NoError(t, err)

		b, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, "summarize", plan)
		require.NoError(t, err)

		assert.Equal(t, a, b)
	})
}

// TestProcessComponentRAGSharing checks that the services of the rag architecture, which declare no
// recommendations, share one deployment per model: same type, provider, model, params and profile.
func TestProcessComponentRAGSharing(t *testing.T) {
	fake := &fakeModelManager{}
	planner := newTestPlanner(t, fake, nil)
	plan := newTestPlan("alice")

	llm := apimodels.Component{
		ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0", Model: "ibm-granite/granite-3.3-8b-instruct",
		Params: map[string]any{"maxModelLen": 8192},
	}
	embedding := apimodels.Component{
		ComponentType: "embedding", ProviderID: "vllm-cpu", Version: "1.0", Model: "ibm-granite/granite-embedding-278m-multilingual",
	}

	llmHashes, embeddingHashes := map[string]bool{}, map[string]bool{}

	for _, svc := range []string{"chat", "digitize", "similarity"} {
		hash, err := planner.processComponent(t.Context(), resolvedComponent{req: embedding}, svc, plan)
		require.NoError(t, err)

		embeddingHashes[hash] = true
	}

	for _, svc := range []string{"chat", "digitize"} {
		hash, err := planner.processComponent(t.Context(), resolvedComponent{req: llm}, svc, plan)
		require.NoError(t, err)

		llmHashes[hash] = true
	}

	assert.Len(t, embeddingHashes, 1, "the embedding model is deployed once for every service")
	assert.Len(t, llmHashes, 1, "the llm is deployed once for chat and digitize")
	assert.Len(t, plan.ModelComponents(), 2, "one llm and one embedding in all")

	for hash := range embeddingHashes {
		assert.Equal(t, []string{"chat", "digitize", "similarity"}, plan.Components[hash].UsedByServices)
	}
}
