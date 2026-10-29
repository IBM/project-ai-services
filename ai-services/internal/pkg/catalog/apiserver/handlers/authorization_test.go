package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

type mockAppRecord struct {
	app     types.Application
	ownerID string
}

type mockAppServiceForAuth struct {
	lastListReq *repository.ListApplicationsRequest
	listResp    *types.ApplicationListResponse
	listErr     error
	apps        []mockAppRecord
}

func (m *mockAppServiceForAuth) ListApplications(_ context.Context, req repository.ListApplicationsRequest) (*types.ApplicationListResponse, error) {
	reqCopy := req
	m.lastListReq = &reqCopy
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResp != nil {
		return m.listResp, nil
	}
	var filtered []types.Application
	for _, record := range m.apps {
		if req.CreatedBy == "" || record.ownerID == req.CreatedBy {
			filtered = append(filtered, record.app)
		}
	}
	return &types.ApplicationListResponse{
		Data: filtered,
	}, nil
}

func (m *mockAppServiceForAuth) UpdateApplication(_ context.Context, _ uuid.UUID, _, _ string) (*types.Application, error) {
	return nil, nil
}

func (m *mockAppServiceForAuth) CreateApplication(_ context.Context, _ apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return nil, nil
}

func (m *mockAppServiceForAuth) GetApplicationByID(_ context.Context, _ uuid.UUID) (*types.Application, error) {
	return nil, nil
}

func (m *mockAppServiceForAuth) GetApplicationResources(_ context.Context, _ uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	return nil, nil
}

func (m *mockAppServiceForAuth) DeleteApplication(_ context.Context, _ uuid.UUID, _ string, _ bool) (*repository.DeleteApplicationResponse, error) {
	return nil, nil
}

func (m *mockAppServiceForAuth) ApplicationsPs(_ context.Context, _ uuid.UUID) (*types.ApplicationPSResponse, error) {
	return nil, nil
}

type mockDatasourceServiceForAuth struct {
	applicationOwner  string
	connectCalled     bool
	disconnectCalled  bool
	lastUserID        string
	lastAppID         uuid.UUID
	lastDatasourceIDs []uuid.UUID
	lastDatasourceID  uuid.UUID
	connectErr        error
	disconnectErr     error
	connectResp       *apimodels.ConnectDatasourcesResponse
}

func (m *mockDatasourceServiceForAuth) CreateDatasource(_ context.Context, _ apimodels.CreateDatasourceRequest) (*apimodels.CreateDatasourceResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) GetDatasource(_ context.Context, _ uuid.UUID) (*apimodels.GetDatasourceResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) ListDatasources(_ context.Context, _ apimodels.ListDatasourcesRequest) (*apimodels.DatasourceListResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) GetDatasourceApplications(_ context.Context, _ uuid.UUID) (*apimodels.DatasourceApplicationsResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) UpdateDatasource(_ context.Context, _ uuid.UUID, _ apimodels.UpdateDatasourceRequest) (*apimodels.UpdateDatasourceResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) DeleteDatasource(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockDatasourceServiceForAuth) ListApplicationDatasources(_ context.Context, _ apimodels.ListApplicationDatasourcesRequest) (*apimodels.ApplicationDatasourceListResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) ConnectDatasourcesToApplication(_ context.Context, applicationID uuid.UUID, userID string, datasourceIDs []uuid.UUID) (*apimodels.ConnectDatasourcesResponse, error) {
	m.connectCalled = true
	m.lastAppID = applicationID
	m.lastUserID = userID
	m.lastDatasourceIDs = datasourceIDs
	if m.applicationOwner != "" && userID != "" && m.applicationOwner != userID {
		return nil, &repository.ValidationError{
			Code:    http.StatusForbidden,
			Message: "user does not own this application",
		}
	}
	if m.connectErr != nil {
		return nil, m.connectErr
	}
	return m.connectResp, nil
}

func (m *mockDatasourceServiceForAuth) GetApplicationDatasource(_ context.Context, _, _ uuid.UUID) (*apimodels.GetApplicationDatasourceResponse, error) {
	return nil, nil
}

func (m *mockDatasourceServiceForAuth) DisconnectDatasourcesFromApplication(_ context.Context, applicationID uuid.UUID, userID string, datasourceID uuid.UUID) error {
	m.disconnectCalled = true
	m.lastAppID = applicationID
	m.lastUserID = userID
	m.lastDatasourceID = datasourceID
	if m.applicationOwner != "" && userID != "" && m.applicationOwner != userID {
		return &repository.ValidationError{
			Code:    http.StatusForbidden,
			Message: "user does not own this application",
		}
	}
	return m.disconnectErr
}

func setupAuthTestRouter(appSvc repository.ApplicationServiceInterface, dsSvc repository.DatasourceServiceInterface, tm *auth.TokenManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authMw := middleware.AuthMiddleware(tm, &repository.NoopTokenBlacklist{})

	appHandler := NewApplicationHandler(appSvc)
	dsHandler := NewDatasourceHandler(dsSvc)

	r.GET("/api/v1/applications/", authMw, appHandler.ListApplications)
	r.PUT("/api/v1/applications/:id/datasources", authMw, dsHandler.ConnectDatasourcesToApplication)
	r.DELETE("/api/v1/applications/:id/datasources/:datasource_id", authMw, dsHandler.DisconnectDatasourcesFromApplication)

	return r
}

func TestListApplications_EnforcesUserOwnershipFilter(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-alice")
	require.NoError(t, err)

	appSvc := &mockAppServiceForAuth{
		listResp: &types.ApplicationListResponse{
			Data: []types.Application{
				{ID: "app-1", Name: "alice-app"},
			},
		},
	}
	router := setupAuthTestRouter(appSvc, &mockDatasourceServiceForAuth{}, tm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, appSvc.lastListReq)
	assert.Equal(t, "user-alice", appSvc.lastListReq.CreatedBy)
}

func TestConnectDatasources_PassesUserIDAndHandlesOwnershipRejection(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-attacker")
	require.NoError(t, err)

	appID := uuid.New()
	dsID := uuid.New()

	dsSvc := &mockDatasourceServiceForAuth{
		connectErr: &repository.ValidationError{
			Code:    http.StatusForbidden,
			Message: "user does not own this application",
		},
	}
	router := setupAuthTestRouter(&mockAppServiceForAuth{}, dsSvc, tm)

	body := `{"datasource_ids":["` + dsID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+appID.String()+"/datasources", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "user does not own this application")
	assert.True(t, dsSvc.connectCalled)
	assert.Equal(t, "user-attacker", dsSvc.lastUserID)
	assert.Equal(t, appID, dsSvc.lastAppID)
	assert.Equal(t, []uuid.UUID{dsID}, dsSvc.lastDatasourceIDs)
}

func TestDisconnectDatasources_PassesUserIDAndHandlesOwnershipRejection(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-attacker")
	require.NoError(t, err)

	appID := uuid.New()
	dsID := uuid.New()

	dsSvc := &mockDatasourceServiceForAuth{
		disconnectErr: &repository.ValidationError{
			Code:    http.StatusForbidden,
			Message: "user does not own this application",
		},
	}
	router := setupAuthTestRouter(&mockAppServiceForAuth{}, dsSvc, tm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+appID.String()+"/datasources/"+dsID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "user does not own this application")
	assert.True(t, dsSvc.disconnectCalled)
	assert.Equal(t, "user-attacker", dsSvc.lastUserID)
	assert.Equal(t, appID, dsSvc.lastAppID)
	assert.Equal(t, dsID, dsSvc.lastDatasourceID)
}

func TestConnectAndDisconnect_SuccessWhenOwner(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-owner")
	require.NoError(t, err)

	appID := uuid.New()
	dsID := uuid.New()

	dsSvc := &mockDatasourceServiceForAuth{}
	router := setupAuthTestRouter(&mockAppServiceForAuth{}, dsSvc, tm)

	// Test Connect
	body := `{"datasource_ids":["` + dsID.String() + `"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/applications/"+appID.String()+"/datasources", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, dsSvc.connectCalled)
	assert.Equal(t, "user-owner", dsSvc.lastUserID)

	// Test Disconnect
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+appID.String()+"/datasources/"+dsID.String(), nil)
	reqDel.Header.Set("Authorization", "Bearer "+token)
	recDel := httptest.NewRecorder()
	router.ServeHTTP(recDel, reqDel)

	assert.Equal(t, http.StatusNoContent, recDel.Code)
	assert.True(t, dsSvc.disconnectCalled)
	assert.Equal(t, "user-owner", dsSvc.lastUserID)
}

func TestCrossUserAuthorization_DistinctPrincipals(t *testing.T) {
	tokenManager := auth.NewTokenManager("local-test-signing-key-32-bytes", 15*time.Minute, 24*time.Hour)

	userAToken, _, err := tokenManager.GenerateAccessToken("user-a")
	require.NoError(t, err)
	userBToken, _, err := tokenManager.GenerateAccessToken("user-b")
	require.NoError(t, err)

	userAID, _, err := tokenManager.ValidateAccessToken(userAToken)
	require.NoError(t, err)
	userBID, _, err := tokenManager.ValidateAccessToken(userBToken)
	require.NoError(t, err)
	require.NotEqual(t, userAID, userBID)

	userAAppID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	datasourceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	appSvc := &mockAppServiceForAuth{
		apps: []mockAppRecord{
			{
				app: types.Application{
					ID:   userAAppID.String(),
					Name: "user-a-application",
				},
				ownerID: userAID,
			},
		},
	}
	dsSvc := &mockDatasourceServiceForAuth{
		applicationOwner: userAID,
	}

	router := setupAuthTestRouter(appSvc, dsSvc, tokenManager)

	// 1. User B lists applications -> only User B's applications are returned (empty list)
	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/applications/", nil)
	listReq.Header.Set("Authorization", "Bearer "+userBToken)
	router.ServeHTTP(listRec, listReq)

	assert.Equal(t, http.StatusOK, listRec.Code)
	assert.NotContains(t, listRec.Body.String(), "user-a-application")
	require.NotNil(t, appSvc.lastListReq)
	assert.Equal(t, userBID, appSvc.lastListReq.CreatedBy)

	// 2. User B tries to connect datasource to User A's application -> 403 Forbidden
	connectRec := httptest.NewRecorder()
	connectReq := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/applications/"+userAAppID.String()+"/datasources",
		strings.NewReader(`{"datasource_ids":["`+datasourceID.String()+`"]}`),
	)
	connectReq.Header.Set("Authorization", "Bearer "+userBToken)
	connectReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(connectRec, connectReq)

	assert.Equal(t, http.StatusForbidden, connectRec.Code)
	assert.Contains(t, connectRec.Body.String(), "user does not own this application")
	assert.True(t, dsSvc.connectCalled)
	assert.Equal(t, userBID, dsSvc.lastUserID)

	// 3. User B tries to disconnect datasource from User A's application -> 403 Forbidden
	disconnectRec := httptest.NewRecorder()
	disconnectReq := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/applications/"+userAAppID.String()+"/datasources/"+datasourceID.String(),
		nil,
	)
	disconnectReq.Header.Set("Authorization", "Bearer "+userBToken)
	router.ServeHTTP(disconnectRec, disconnectReq)

	assert.Equal(t, http.StatusForbidden, disconnectRec.Code)
	assert.Contains(t, disconnectRec.Body.String(), "user does not own this application")
	assert.True(t, dsSvc.disconnectCalled)
	assert.Equal(t, userBID, dsSvc.lastUserID)

	// 4. User A (owner) connects datasource to User A's application -> 204 No Content
	ownerConnectRec := httptest.NewRecorder()
	ownerConnectReq := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/applications/"+userAAppID.String()+"/datasources",
		strings.NewReader(`{"datasource_ids":["`+datasourceID.String()+`"]}`),
	)
	ownerConnectReq.Header.Set("Authorization", "Bearer "+userAToken)
	ownerConnectReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(ownerConnectRec, ownerConnectReq)

	assert.Equal(t, http.StatusNoContent, ownerConnectRec.Code)

	// 5. User A (owner) disconnects datasource from User A's application -> 204 No Content
	ownerDisconnectRec := httptest.NewRecorder()
	ownerDisconnectReq := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/applications/"+userAAppID.String()+"/datasources/"+datasourceID.String(),
		nil,
	)
	ownerDisconnectReq.Header.Set("Authorization", "Bearer "+userAToken)
	router.ServeHTTP(ownerDisconnectRec, ownerDisconnectReq)

	assert.Equal(t, http.StatusNoContent, ownerDisconnectRec.Code)
}
