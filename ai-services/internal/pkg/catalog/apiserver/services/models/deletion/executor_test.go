package deletion

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callLog records the order of calls made by the fakes of one test, across the gateway and the
// deleter.
type callLog struct{ calls []string }

func (l *callLog) add(call string) { l.calls = append(l.calls, call) }

// fakeLiteLLM is a LiteLLMClient that records every call and fails the one named in failOn.
type fakeLiteLLM struct {
	catalogclient.LiteLLMClient
	log    *callLog
	failOn string
}

func (f *fakeLiteLLM) call(name string) error {
	f.log.add(name)
	if f.failOn == name {
		return errors.New(name + " failed")
	}

	return nil
}

func (f *fakeLiteLLM) DeleteKeysByAlias(context.Context, []string) error { return f.call("key") }

func (f *fakeLiteLLM) DeregisterPassThrough(context.Context, string) error {
	return f.call("passthrough")
}

func (f *fakeLiteLLM) DeregisterModel(context.Context, string) error { return f.call("model") }

// fakeDeleter is a Deleter that records the call, fails with err, or panics.
type fakeDeleter struct {
	log      *callLog
	err      error
	panicMsg string
	gotRT    runtime.Runtime
	gotID    uuid.UUID
}

func (f *fakeDeleter) Delete(_ context.Context, rt runtime.Runtime, id uuid.UUID) error {
	f.log.add("pods")
	f.gotRT, f.gotID = rt, id

	if f.panicMsg != "" {
		panic(f.panicMsg)
	}

	return f.err
}

type statusUpdate struct {
	id      uuid.UUID
	status  dbmodels.ComponentStatus
	message string
}

// fakeComponentRepo records status updates.
type fakeComponentRepo struct {
	dbrepo.ComponentRepository
	updates   []statusUpdate
	updateErr error
}

func (f *fakeComponentRepo) UpdateStatus(_ context.Context, id uuid.UUID, status dbmodels.ComponentStatus, message string) error {
	f.updates = append(f.updates, statusUpdate{id, status, message})

	return f.updateErr
}

// fakeConnectorRepo returns deleteErr from DeleteIfUnlinked and records the call.
type fakeConnectorRepo struct {
	dbrepo.ConnectorRepository
	log       *callLog
	deleteErr error
	gotID     uuid.UUID
	gotDep    dbmodels.DependencyType
}

func (f *fakeConnectorRepo) DeleteIfUnlinked(_ context.Context, id uuid.UUID, dep dbmodels.DependencyType) (int, error) {
	f.log.add("connector")
	f.gotID, f.gotDep = id, dep

	return 0, f.deleteErr
}

var fullRoute = litellm.Route{LiteLLMModelID: "litellm-1", KeyAlias: "ibm/granite-id-1", Tag: "id-1", Provider: "watsonx"}

func newTestExecutor(log *callLog, failOn string, compRepo *fakeComponentRepo, connRepo *fakeConnectorRepo, deleters map[runtimeTypes.RuntimeType]Deleter) *Executor {
	return &Executor{
		componentRepo: compRepo,
		connectorRepo: connRepo,
		gateway:       litellm.NewGateway(&fakeLiteLLM{log: log, failOn: failOn}),
		deleters:      deleters,
	}
}

func TestNewExecutor_RegistersDeleters(t *testing.T) {
	e := NewExecutor(&fakeComponentRepo{}, &fakeConnectorRepo{}, &fakeLiteLLM{log: &callLog{}})

	assert.Contains(t, e.deleters, runtimeTypes.RuntimeTypePodman)
	assert.Contains(t, e.deleters, runtimeTypes.RuntimeTypeOpenShift)
}

func TestExecutor_ExecuteLocal(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name         string
		rtType       runtimeTypes.RuntimeType
		failOn       string // gateway step that fails
		deleterErr   error
		deleterPanic string
		updateErr    error
		wantCalls    []string
		wantMessage  string // "" means the delete succeeded and the status is left alone
	}{
		{
			name:      "gateway is cleared first, then the pods are deleted",
			rtType:    runtimeTypes.RuntimeTypePodman,
			wantCalls: []string{"key", "passthrough", "model", "pods"},
		},
		{
			name:        "a gateway failure keeps the pods and marks the component errored",
			rtType:      runtimeTypes.RuntimeTypePodman,
			failOn:      "passthrough",
			wantCalls:   []string{"key", "passthrough"},
			wantMessage: `failed to deregister tokenize passthrough "watsonx-token-passthrough-id-1": passthrough failed`,
		},
		{
			name:        "a pod deletion failure marks the component errored",
			rtType:      runtimeTypes.RuntimeTypePodman,
			deleterErr:  errors.New("failed to delete 1 pod(s)"),
			wantCalls:   []string{"key", "passthrough", "model", "pods"},
			wantMessage: "failed to delete 1 pod(s)",
		},
		{
			name:         "a panic is recovered and marks the component errored",
			rtType:       runtimeTypes.RuntimeTypePodman,
			deleterPanic: "boom",
			wantCalls:    []string{"key", "passthrough", "model", "pods"},
			wantMessage:  "panic: boom",
		},
		{
			name:        "a runtime without a deleter marks the component errored after the gateway is cleared",
			rtType:      runtimeTypes.RuntimeTypeOpenShift,
			wantCalls:   []string{"key", "passthrough", "model"},
			wantMessage: "models are unsupported on openshift runtime",
		},
		{
			name:        "a failing status update does not panic",
			rtType:      runtimeTypes.RuntimeTypePodman,
			deleterErr:  errors.New("boom"),
			updateErr:   errors.New("db down"),
			wantCalls:   []string{"key", "passthrough", "model", "pods"},
			wantMessage: "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &callLog{}
			compRepo := &fakeComponentRepo{updateErr: tt.updateErr}
			deleter := &fakeDeleter{log: log, err: tt.deleterErr, panicMsg: tt.deleterPanic}
			e := newTestExecutor(log, tt.failOn, compRepo, &fakeConnectorRepo{log: log},
				map[runtimeTypes.RuntimeType]Deleter{runtimeTypes.RuntimeTypePodman: deleter})
			rt := mock.New(tt.rtType)

			e.ExecuteLocal(context.Background(), &types.LocalPlan{ComponentID: id, Runtime: rt, Route: fullRoute})

			assert.Equal(t, tt.wantCalls, log.calls)

			if tt.wantMessage == "" {
				assert.Empty(t, compRepo.updates, "the deleter removes the row, so the status is not touched")
				assert.Equal(t, id, deleter.gotID)
				assert.Equal(t, runtime.Runtime(rt), deleter.gotRT)

				return
			}

			require.Len(t, compRepo.updates, 1)
			assert.Equal(t, statusUpdate{id, dbmodels.ComponentStatusError, tt.wantMessage}, compRepo.updates[0])
		})
	}
}

func TestExecutor_ExecuteRemote(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name      string
		failOn    string
		deleteErr error
		wantCalls []string
		wantCode  int    // ValidationError code, 0 when not a ValidationError
		wantErr   string // exact message of a plain (500) error
	}{
		{
			name:      "gateway is cleared, then the connector row is deleted",
			wantCalls: []string{"key", "passthrough", "model", "connector"},
		},
		{
			name:      "a gateway failure keeps the connector",
			failOn:    "model",
			wantCalls: []string{"key", "passthrough", "model"},
			wantErr:   `failed to deregister litellm model "litellm-1": model failed`,
		},
		{
			name:      "connector already gone is a 404",
			deleteErr: dbrepo.ErrConnectorNotFound,
			wantCalls: []string{"key", "passthrough", "model", "connector"},
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "connector linked to a service is a 409",
			deleteErr: dbrepo.ErrConnectorInUse,
			wantCalls: []string{"key", "passthrough", "model", "connector"},
			wantCode:  http.StatusConflict,
		},
		{
			name:      "any other repository failure is wrapped as an internal error",
			deleteErr: errors.New("db down"),
			wantCalls: []string{"key", "passthrough", "model", "connector"},
			wantErr:   "failed to delete connector: db down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &callLog{}
			connRepo := &fakeConnectorRepo{log: log, deleteErr: tt.deleteErr}
			e := newTestExecutor(log, tt.failOn, &fakeComponentRepo{}, connRepo, nil)

			err := e.ExecuteRemote(context.Background(), &types.RemotePlan{ConnectorID: id, Route: fullRoute})

			assert.Equal(t, tt.wantCalls, log.calls)

			var verr *validators.ValidationError

			switch {
			case tt.wantCode != 0:
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, tt.wantCode, verr.Code)
			case tt.wantErr != "":
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err.Error())
				assert.False(t, errors.As(err, &verr), "an infrastructure failure must stay a 500")
			default:
				require.NoError(t, err)
			}

			if tt.wantCalls[len(tt.wantCalls)-1] == "connector" {
				assert.Equal(t, id, connRepo.gotID)
				assert.Equal(t, dbmodels.DependencyTypeConnector, connRepo.gotDep)
			}
		})
	}
}
