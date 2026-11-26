package openshift

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeComponentRepo records the rows it was asked to delete and returns stubbed components.
type fakeComponentRepo struct {
	dbrepo.ComponentRepository
	comp      *dbmodels.Component
	getErr    error
	deleted   []uuid.UUID
	deleteErr error
}

func (f *fakeComponentRepo) GetByID(_ context.Context, id uuid.UUID) (*dbmodels.Component, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}

	if f.comp != nil {
		return f.comp, nil
	}

	return &dbmodels.Component{ID: id, Type: "llm"}, nil
}

func (f *fakeComponentRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)

	return f.deleteErr
}

type fakeRegistry struct {
	stream.WorkerRegistry
	cmdChan      chan *workerpb.Command
	uninstallErr string
}

func (f *fakeRegistry) WorkerCommandChannel(_ string) (chan *workerpb.Command, bool) {
	return f.cmdChan, true
}

func (f *fakeRegistry) WaitForResult(_, commandID string) (chan *workerpb.CommandResult, error) {
	ch := make(chan *workerpb.CommandResult, 1)
	if f.uninstallErr != "" {
		ch <- &workerpb.CommandResult{
			CommandId: commandID,
			Success:   false,
			Error:     f.uninstallErr,
		}
	} else {
		ch <- &workerpb.CommandResult{
			CommandId: commandID,
			Success:   true,
		}
	}

	return ch, nil
}

func TestOpenShiftDeleter_Delete(t *testing.T) {
	id := uuid.New()
	wantRelease := catalogutils.ModelHelmReleaseName(id, "llm")

	tests := []struct {
		name         string
		getErr       error
		uninstallErr string
		deleteErr    error
		wantErr      string
		wantRowGone  bool
	}{
		{
			name:        "the release is uninstalled and the component row is removed",
			wantRowGone: true,
		},
		{
			name:    "a component that cannot be retrieved from DB fails early",
			getErr:  errors.New("db connection failed"),
			wantErr: "failed to get component " + id.String() + " from DB: db connection failed",
		},
		{
			name:         "a release that cannot be uninstalled keeps the row",
			uninstallErr: "helm error",
			wantErr:      "failed to uninstall model release '" + wantRelease + "'",
		},
		{
			name:        "a row that cannot be deleted from DB reports error",
			deleteErr:   errors.New("db delete failed"),
			wantErr:     "failed to delete component " + id.String() + " from DB: db delete failed",
			wantRowGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &fakeRegistry{
				cmdChan:      make(chan *workerpb.Command, 10),
				uninstallErr: tt.uninstallErr,
			}
			rt := remoteruntime.New("worker-1", runtimeTypes.RuntimeTypeOpenShift, reg)
			repo := &fakeComponentRepo{
				getErr:    tt.getErr,
				deleteErr: tt.deleteErr,
			}

			deleter := NewOpenShiftDeleter(repo)

			err := deleter.Delete(context.Background(), rt, id)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}

			if tt.wantRowGone {
				assert.Equal(t, []uuid.UUID{id}, repo.deleted)
			} else {
				assert.Empty(t, repo.deleted)
			}
		})
	}
}
