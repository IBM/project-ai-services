package openshift

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeComponentRepo records the rows it was asked to delete.
type fakeComponentRepo struct {
	dbrepo.ComponentRepository
	deleted   []uuid.UUID
	deleteErr error
}

func (f *fakeComponentRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)

	return f.deleteErr
}

// namespaceRuntime wraps the mock OpenShift runtime to record the namespace it is asked to delete
// and to fail the calls the deleter makes.
type namespaceRuntime struct {
	runtime.Runtime
	deletedNamespace string
	listErr          error
	deleteNsErr      error
}

func (r *namespaceRuntime) ListCRD(context.Context, *unstructured.UnstructuredList, map[string][]string) ([]runtimeTypes.CRDResource, error) {
	return nil, r.listErr
}

func (r *namespaceRuntime) DeleteNamespace(_ context.Context, name string) error {
	r.deletedNamespace = name

	return r.deleteNsErr
}

func TestOpenShiftDeleter_Delete(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name        string
		rt          *namespaceRuntime
		repoErr     error
		wantErr     string
		wantNsDrop  bool // the model's namespace was deleted
		wantRowGone bool // the component row was deleted
	}{
		{
			name:        "the namespace and the row are deleted",
			rt:          &namespaceRuntime{},
			wantNsDrop:  true,
			wantRowGone: true,
		},
		{
			name:    "a serving runtime that cannot be listed keeps the namespace and the row",
			rt:      &namespaceRuntime{listErr: errors.New("api down")},
			wantErr: "failed to list serving runtimes: api down",
		},
		{
			name:       "a namespace that cannot be deleted keeps the row",
			rt:         &namespaceRuntime{deleteNsErr: errors.New("forbidden")},
			wantErr:    "failed to delete 'ai-services-model-" + id.String()[:8] + "' namespace: forbidden",
			wantNsDrop: true,
		},
		{
			name:        "a row that cannot be deleted is reported",
			rt:          &namespaceRuntime{},
			repoErr:     errors.New("db down"),
			wantErr:     "failed to delete component " + id.String() + " from DB: db down",
			wantNsDrop:  true,
			wantRowGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.rt.Runtime = mock.New(runtimeTypes.RuntimeTypeOpenShift)
			repo := &fakeComponentRepo{deleteErr: tt.repoErr}

			err := NewOpenShiftDeleter(repo).Delete(context.Background(), tt.rt, id)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}

			if tt.wantNsDrop {
				assert.Equal(t, catalogutils.ModelNamespace(id), tt.rt.deletedNamespace)
			} else {
				assert.Empty(t, tt.rt.deletedNamespace)
			}

			if tt.wantRowGone {
				assert.Equal(t, []uuid.UUID{id}, repo.deleted)
			} else {
				assert.Empty(t, repo.deleted)
			}
		})
	}
}
