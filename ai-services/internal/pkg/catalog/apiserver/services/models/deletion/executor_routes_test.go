package deletion

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRoutes is a RouteRemover that records the call in the shared log.
type fakeRoutes struct {
	log   *callLog
	err   error
	gotID uuid.UUID
}

func (f *fakeRoutes) Unregister(_ context.Context, _ runtime.Runtime, id uuid.UUID) error {
	f.log.add("routes")
	f.gotID = id

	return f.err
}

func TestExecutor_ExecuteLocal_RemoteWorkerRoutes(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name        string
		workerName  string
		routesErr   error
		wantCalls   []string
		wantRoutes  bool
		wantMessage string
	}{
		{
			name:       "a remote model's routes are removed after the gateway and before the pods",
			workerName: "w1",
			wantCalls:  []string{"key", "passthrough", "model", "routes", "pods"},
			wantRoutes: true,
		},
		{
			name:       "the local worker has no private routes to remove",
			workerName: "Local",
			wantCalls:  []string{"key", "passthrough", "model", "pods"},
		},
		{
			name:       "an unnamed worker is the local worker",
			workerName: "",
			wantCalls:  []string{"key", "passthrough", "model", "pods"},
		},
		{
			name:        "a route removal failure keeps the pods so the delete can be retried",
			workerName:  "w1",
			routesErr:   errors.New("worker caddy down"),
			wantCalls:   []string{"key", "passthrough", "model", "routes"},
			wantRoutes:  true,
			wantMessage: `failed to remove routes of model ` + id.String() + ` on worker "w1": worker caddy down`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &callLog{}
			compRepo := &fakeComponentRepo{}
			deleter := &fakeDeleter{log: log}
			routes := &fakeRoutes{log: log, err: tt.routesErr}
			e := newTestExecutor(log, "", compRepo, &fakeConnectorRepo{log: log},
				map[runtimeTypes.RuntimeType]Deleter{runtimeTypes.RuntimeTypePodman: deleter})
			e.routes = routes

			e.ExecuteLocal(context.Background(), &types.LocalPlan{
				ComponentID: id,
				WorkerName:  tt.workerName,
				Runtime:     mock.New(runtimeTypes.RuntimeTypePodman),
				Route:       fullRoute,
			})

			assert.Equal(t, tt.wantCalls, log.calls)

			if tt.wantRoutes {
				assert.Equal(t, id, routes.gotID)
			}

			if tt.wantMessage == "" {
				assert.Empty(t, compRepo.updates)

				return
			}

			require.Len(t, compRepo.updates, 1)
			assert.Equal(t, tt.wantMessage, compRepo.updates[0].message)
		})
	}
}
