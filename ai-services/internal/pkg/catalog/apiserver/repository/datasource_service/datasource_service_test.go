package datasourceservice

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAppRepoForTest struct {
	app *dbmodels.Application
	err error
}

func (m *mockAppRepoForTest) GetAll(_ context.Context, _ *dbrepo.ApplicationFilters) ([]dbmodels.Application, error) {
	return nil, nil
}
func (m *mockAppRepoForTest) GetCount(_ context.Context, _ *dbrepo.ApplicationFilters) (int, error) {
	return 0, nil
}
func (m *mockAppRepoForTest) GetByID(_ context.Context, _ uuid.UUID) (*dbmodels.Application, error) {
	return m.app, m.err
}
func (m *mockAppRepoForTest) GetByName(_ context.Context, _ string) (*dbmodels.Application, error) {
	return nil, nil
}
func (m *mockAppRepoForTest) Insert(_ context.Context, _ *dbmodels.Application) error {
	return nil
}
func (m *mockAppRepoForTest) UpdateDeploymentName(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (m *mockAppRepoForTest) UpdateStatus(_ context.Context, _ uuid.UUID, _ dbmodels.ApplicationStatus, _ string) error {
	return nil
}
func (m *mockAppRepoForTest) Delete(_ context.Context, _ uuid.UUID) error {
	return nil
}

func TestConnectDatasourcesToApplication_OwnershipCheck(t *testing.T) {
	appID := uuid.New()
	dsID := uuid.New()

	app := &dbmodels.Application{
		ID:        appID,
		Name:      "test-app",
		CreatedBy: "victim-user",
	}

	appRepo := &mockAppRepoForTest{app: app}
	svc := &DatasourceService{
		appRepo: appRepo,
	}

	// 1. Attacker calls connect on victim-owned application -> 403 Forbidden
	resp, err := svc.ConnectDatasourcesToApplication(context.Background(), appID, "attacker-user", []uuid.UUID{dsID})
	assert.Nil(t, resp)
	require.Error(t, err)

	valErr, ok := err.(*ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, valErr.Code)
	assert.Equal(t, "user does not own this application", valErr.Message)
}

func TestDisconnectDatasourcesFromApplication_OwnershipCheck(t *testing.T) {
	appID := uuid.New()
	dsID := uuid.New()

	app := &dbmodels.Application{
		ID:        appID,
		Name:      "test-app",
		CreatedBy: "victim-user",
	}

	appRepo := &mockAppRepoForTest{app: app}
	svc := &DatasourceService{
		appRepo: appRepo,
	}

	// 1. Attacker calls disconnect on victim-owned application -> 403 Forbidden
	err := svc.DisconnectDatasourcesFromApplication(context.Background(), appID, "attacker-user", dsID)
	require.Error(t, err)

	valErr, ok := err.(*ValidationError)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, valErr.Code)
	assert.Equal(t, "user does not own this application", valErr.Message)
}

// mockConnectorRepoForTest embeds the interface so only GetByIDs needs to be implemented.
type mockConnectorRepoForTest struct {
	dbrepo.ConnectorRepository
}

func (m *mockConnectorRepoForTest) GetByIDs(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]dbmodels.Connector, error) {
	return nil, nil
}

func TestBuildDatasourcePage_PreservesOrder(t *testing.T) {
	const n = 5

	order := make([]string, n)
	for i := range order {
		order[i] = uuid.New().String()
	}

	// Insert in the reverse of the expected order. Go map iteration for small maps is a
	// rotation of insertion order, so ranging over the map can never reproduce `order`.
	serviceItems := make(map[string]apimodels.ConnectorItem, n)
	for i := n - 1; i >= 0; i-- {
		serviceItems[order[i]] = apimodels.ConnectorItem{ID: order[i]}
	}

	svc := &DatasourceService{connectorRepo: &mockConnectorRepoForTest{}}

	items, err := svc.buildDatasourcePage(context.Background(), "http://service", serviceItems, order)
	require.NoError(t, err)
	require.Len(t, items, n)

	for i, item := range items {
		assert.Equal(t, order[i], item.ID)
	}
}
