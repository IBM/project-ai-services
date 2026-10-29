package datasourceservice

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
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
