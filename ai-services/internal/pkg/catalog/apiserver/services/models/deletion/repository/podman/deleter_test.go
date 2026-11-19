package podman

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeComponentRepo records the rows it was asked to delete and the statuses it was given.
type fakeComponentRepo struct {
	dbrepo.ComponentRepository
	deleted   []uuid.UUID
	deleteErr error
	statuses  []dbmodels.ComponentStatus
}

func (f *fakeComponentRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)

	return f.deleteErr
}

func (f *fakeComponentRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status dbmodels.ComponentStatus, _ string) error {
	f.statuses = append(f.statuses, status)

	return nil
}

// failingRuntime fails ListPods, so the component can be neither inspected nor cleaned up.
type failingRuntime struct{ runtime.Runtime }

func (failingRuntime) ListPods(context.Context, map[string][]string) ([]runtimeTypes.Pod, error) {
	return nil, errors.New("podman unreachable")
}

// modelManifest is a model component as the deployer creates it: a pod labelled with its
// component ID, the secret and the volume it uses.
func modelManifest(id uuid.UUID) string {
	return strings.NewReplacer("{id}", id.String()).Replace(`
apiVersion: v1
kind: Secret
metadata:
  name: model-secret
---
apiVersion: v1
kind: Pod
metadata:
  name: model-pod
  labels:
    ai-services.io/template: "{id}"
    ai-services.io/secret: model-secret
    ai-services.io/volume: model-volume
spec:
  volumes:
    - name: weights
      persistentVolumeClaim:
        claimName: model-volume
  containers:
    - name: vllm
      image: vllm:latest
`)
}

func TestPodmanDeleter_Delete(t *testing.T) {
	id := uuid.New()
	otherID := uuid.New()

	tests := []struct {
		name         string
		manifestFor  *uuid.UUID // component the pod is labelled for, nil for no pods at all
		deleteErr    error
		failingList  bool
		wantErr      string
		wantRowGone  bool
		wantStatus   []dbmodels.ComponentStatus
		wantOthersOK bool // the pod of another component must survive
	}{
		{
			name:        "pods, secrets and volumes are removed, then the row",
			manifestFor: &id,
			wantRowGone: true,
		},
		{
			name:        "a model that never got pods still has its row removed",
			wantRowGone: true,
		},
		{
			name:         "pods of another component are left alone",
			manifestFor:  &otherID,
			wantRowGone:  true,
			wantOthersOK: true,
		},
		{
			name:        "a row delete failure is reported and the component is marked errored",
			manifestFor: &id,
			deleteErr:   errors.New("db down"),
			wantErr:     "failed to delete from DB: db down",
			wantRowGone: true,
			wantStatus:  []dbmodels.ComponentStatus{dbmodels.ComponentStatusError},
		},
		{
			name:        "an unreachable runtime keeps the row and marks the component errored",
			failingList: true,
			wantErr:     "failed to list pods: podman unreachable",
			wantStatus:  []dbmodels.ComponentStatus{dbmodels.ComponentStatusError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo := &fakeComponentRepo{deleteErr: tt.deleteErr}
			mockRT := mock.New(runtimeTypes.RuntimeTypePodman)

			if tt.manifestFor != nil {
				_, err := mockRT.CreatePod(ctx, strings.NewReader(modelManifest(*tt.manifestFor)), nil)
				require.NoError(t, err)
			}

			var rt runtime.Runtime = mockRT
			if tt.failingList {
				rt = failingRuntime{rt}
			}

			err := NewPodmanDeleter(repo).Delete(ctx, rt, id)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}

			if tt.wantRowGone {
				assert.Equal(t, []uuid.UUID{id}, repo.deleted)
			} else {
				assert.Empty(t, repo.deleted, "the row must be kept when the runtime cleanup failed")
			}
			assert.Equal(t, tt.wantStatus, repo.statuses)

			if tt.failingList {
				return
			}

			ownPods, listErr := mockRT.ListPods(ctx, map[string][]string{"label": {"ai-services.io/template=" + id.String()}})
			require.NoError(t, listErr)
			assert.Empty(t, ownPods)

			if tt.wantOthersOK {
				otherPods, listErr := mockRT.ListPods(ctx, map[string][]string{"label": {"ai-services.io/template=" + otherID.String()}})
				require.NoError(t, listErr)
				assert.Len(t, otherPods, 1, "another component's pods must not be touched")

				return
			}

			secretExists, _ := mockRT.SecretExists(ctx, "model-secret")
			volumeExists, _ := mockRT.VolumeExists(ctx, "model-volume")
			assert.False(t, secretExists, "the model's secret must be removed")
			assert.False(t, volumeExists, "the model's data is deleted with it (keepData=false)")
		})
	}
}
