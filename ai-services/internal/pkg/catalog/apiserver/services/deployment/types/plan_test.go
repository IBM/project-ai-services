package types

import (
	"context"
	"testing"

	"github.com/google/uuid"

	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentPlanModelComponents(t *testing.T) {
	plan := &DeploymentPlan{Components: map[string]*ComponentPlan{
		"c": {Hash: "c", Model: &modeltypes.LocalPlan{}},
		"a": {Hash: "a", Model: &modeltypes.LocalPlan{}},
		"b": {Hash: "b"},
		"d": {Hash: "d", PreDeployed: true},
	}}

	got := plan.ModelComponents()

	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Hash, "components are returned in hash order")
	assert.Equal(t, "c", got[1].Hash)
}

func TestComponentPlanIsModel(t *testing.T) {
	assert.True(t, (&ComponentPlan{Model: &modeltypes.LocalPlan{}}).IsModel())
	assert.True(t, (&ComponentPlan{PreDeployed: true}).IsModel())
	assert.False(t, (&ComponentPlan{}).IsModel())
}

func TestDeploymentPlanNonModelComponents(t *testing.T) {
	plan := &DeploymentPlan{Components: map[string]*ComponentPlan{
		"a": {Hash: "a", Model: &modeltypes.LocalPlan{}},
		"p": {Hash: "p", PreDeployed: true},
		"x": {Hash: "x", ComponentType: "vector_store"},
	}}

	got := plan.NonModelComponents()

	require.Len(t, got, 1)
	assert.Contains(t, got, "x")
}

func TestDeploymentPlanComponentIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	plan := &DeploymentPlan{Components: map[string]*ComponentPlan{
		"a": {DatabaseID: a},
		"b": {DatabaseID: b},
	}}

	assert.Equal(t, map[string]uuid.UUID{"a": a, "b": b}, plan.ComponentIDs())
}

// fakeComponents records the status each component is given.
type fakeComponents struct {
	repository.ComponentRepository

	statuses map[uuid.UUID]dbmodels.ComponentStatus
}

func (f *fakeComponents) UpdateStatus(_ context.Context, id uuid.UUID, status dbmodels.ComponentStatus, _ string) error {
	f.statuses[id] = status

	return nil
}

func TestDeploymentPlanComponentStatusFunc(t *testing.T) {
	deployed, model, existing := uuid.New(), uuid.New(), uuid.New()
	plan := &DeploymentPlan{Components: map[string]*ComponentPlan{
		"x": {DatabaseID: deployed},
		"m": {DatabaseID: model, Model: &modeltypes.LocalPlan{}},
		"p": {DatabaseID: existing, PreDeployed: true},
	}}
	repo := &fakeComponents{statuses: map[uuid.UUID]dbmodels.ComponentStatus{}}
	setStatus := plan.ComponentStatusFunc(repo, dbmodels.ComponentStatusRunning)

	for _, id := range []uuid.UUID{deployed, model, existing} {
		require.NoError(t, setStatus(t.Context(), id, ""))
	}

	assert.Equal(t, map[uuid.UUID]dbmodels.ComponentStatus{deployed: dbmodels.ComponentStatusRunning}, repo.statuses,
		"the status of a model is reported by the model service, and an existing instance is untouched")
}
