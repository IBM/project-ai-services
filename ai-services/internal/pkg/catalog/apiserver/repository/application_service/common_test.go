package applicationservice

import (
	"context"
	"errors"

	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDependencyRepo records the dependencies it is asked to add.
type recordingDependencyRepo struct {
	dbrepo.ServiceDependencyRepository

	added []*models.ServiceDependency
}

func (r *recordingDependencyRepo) AddDependency(_ context.Context, dependency *models.ServiceDependency) error {
	r.added = append(r.added, dependency)

	return nil
}

func TestInsertServiceDependencies(t *testing.T) {
	serviceID := uuid.New()
	localID, remoteID := uuid.New(), uuid.New()
	components := map[string]*deployment.ComponentPlan{
		"local":  {PreDeployed: true},
		"remote": {PreDeployed: true, Remote: true},
	}
	ids := map[string]uuid.UUID{"local": localID, "remote": remoteID}

	repo := &recordingDependencyRepo{}
	s := &ApplicationServiceBase{ServiceDependencyRepo: repo}

	require.NoError(t, s.insertServiceDependencies(t.Context(), serviceID, []string{"local", "remote"}, ids, components))

	require.Len(t, repo.added, 2)
	assert.Equal(t, models.ServiceDependency{ServiceID: serviceID, DependencyID: localID, DependencyType: models.DependencyTypeComponent}, *repo.added[0])
	assert.Equal(t, models.ServiceDependency{ServiceID: serviceID, DependencyID: remoteID, DependencyType: models.DependencyTypeConnector}, *repo.added[1],
		"a remote model is linked as the connector it was registered as")
}

func TestCollectDatasourceIDs_SkipsOtherConnectorTypes(t *testing.T) {
	datasource, model := uuid.New(), uuid.New()
	services := []apimodels.Service{
		{Connectors: []apimodels.ConnectorRef{
			{ID: datasource.String(), Type: constants.ConnectorTypeDatasource},
			{ID: model.String(), Type: constants.ConnectorTypeModels},
		}},
		{Connectors: []apimodels.ConnectorRef{
			{ID: datasource.String(), Type: constants.ConnectorTypeDatasource},
		}},
	}

	got := collectDatasourceIDs(t.Context(), services)

	assert.Equal(t, []uuid.UUID{datasource}, got, "only datasources, once each")
}

// dependenciesByService is a ServiceDependencyRepository that answers GetDependenciesByServiceID.
type dependenciesByService struct {
	dbrepo.ServiceDependencyRepository

	deps map[uuid.UUID][]models.ServiceDependency
	err  error
}

func (d *dependenciesByService) GetDependenciesByServiceID(_ context.Context, serviceID uuid.UUID) ([]models.ServiceDependency, error) {
	return d.deps[serviceID], d.err
}

// revokingModels is a ModelManager that records what RevokeGatewayKeys is asked to revoke.
type revokingModels struct {
	deployment.ModelManager

	appID uuid.UUID
	ids   []uuid.UUID
	err   error
	calls int
}

func (r *revokingModels) RevokeGatewayKeys(_ context.Context, applicationID uuid.UUID, ids []uuid.UUID) error {
	r.calls++
	r.appID, r.ids = applicationID, ids

	return r.err
}

func TestCollectDependencyIDs(t *testing.T) {
	svc1, svc2 := uuid.New(), uuid.New()
	shared, component, connector := uuid.New(), uuid.New(), uuid.New()

	repo := &dependenciesByService{deps: map[uuid.UUID][]models.ServiceDependency{
		svc1: {
			{ServiceID: svc1, DependencyID: component, DependencyType: models.DependencyTypeComponent},
			{ServiceID: svc1, DependencyID: shared, DependencyType: models.DependencyTypeConnector},
		},
		svc2: {
			{ServiceID: svc2, DependencyID: shared, DependencyType: models.DependencyTypeConnector},
			{ServiceID: svc2, DependencyID: connector, DependencyType: models.DependencyTypeConnector},
		},
	}}
	s := &ApplicationServiceBase{ServiceDependencyRepo: repo}

	got, err := s.collectDependencyIDs(t.Context(), []models.Service{{ID: svc1}, {ID: svc2}})

	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{component, shared, connector}, got, "components and connectors, once each")

	repo.err = errors.New("db down")
	_, err = s.collectDependencyIDs(t.Context(), []models.Service{{ID: svc1}})
	require.ErrorIs(t, err, repo.err)
}

func TestRevokeGatewayKeys(t *testing.T) {
	appID := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New()}

	t.Run("revokes the application's keys through the model manager", func(t *testing.T) {
		models := &revokingModels{}
		s := &ApplicationServiceBase{Models: models}

		require.NoError(t, s.revokeGatewayKeys(t.Context(), appID, ids))

		assert.Equal(t, appID, models.appID)
		assert.Equal(t, ids, models.ids)
	})

	t.Run("a failure is returned", func(t *testing.T) {
		boom := errors.New("gateway down")
		s := &ApplicationServiceBase{Models: &revokingModels{err: boom}}

		require.ErrorIs(t, s.revokeGatewayKeys(t.Context(), appID, ids), boom)
	})

	t.Run("without a model manager there is nothing to revoke", func(t *testing.T) {
		s := &ApplicationServiceBase{}

		require.NoError(t, s.revokeGatewayKeys(t.Context(), appID, ids))
	})
}
