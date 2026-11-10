package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockModelService struct {
	createModelFunc func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error)
	listModelsFunc  func(ctx context.Context, req models.ListModelsRequest) (*models.ModelListResponse, error)
	getModelFunc    func(ctx context.Context, id uuid.UUID, createdBy string) (*models.ModelDetail, error)
}

func (m *mockModelService) GetModelByID(ctx context.Context, id uuid.UUID, createdBy string) (*models.ModelDetail, error) {
	if m.getModelFunc != nil {
		return m.getModelFunc(ctx, id, createdBy)
	}

	return &models.ModelDetail{ModelBase: models.ModelBase{ID: id.String()}}, nil
}

func (m *mockModelService) ListModels(ctx context.Context, req models.ListModelsRequest) (*models.ModelListResponse, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx, req)
	}

	return &models.ModelListResponse{Data: []models.ModelListItem{}}, nil
}

func (m *mockModelService) CreateModel(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
	if m.createModelFunc != nil {
		return m.createModelFunc(ctx, req)
	}

	return &models.CreateModelResponse{ID: "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f"}, nil
}

func setupModelRouter(svc repository.ModelServiceInterface, tm *auth.TokenManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authMw := middleware.AuthMiddleware(tm, &repository.NoopTokenBlacklist{})
	h := NewModelHandler(svc)
	router.POST("/api/v1/models", authMw, h.CreateModel)
	router.GET("/api/v1/models", authMw, h.ListModels)
	router.GET("/api/v1/models/:id", authMw, h.GetModelByID)

	return router
}

// validModelPayload is a request that passes the handler's binding rules.
func validModelPayload() models.CreateModelRequest {
	return models.CreateModelRequest{
		DeploymentType: constants.DeploymentTypeLocal,
		ComponentType:  "llm",
		Name:           "granite-llm",
		ProviderID:     "vllm-spyre",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		Profile:        "compact-4k",
	}
}

func TestCreateModel(t *testing.T) {
	const (
		localID  = "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f"
		remoteID = "c1d2e3f4-a5b6-7890-cdef-123456789abc"
	)

	tests := []struct {
		name string
		// rawBody, when set, is sent as is instead of the (mutated) valid payload.
		rawBody string
		mutate  func(*models.CreateModelRequest)
		noAuth  bool
		// svcErr is what the model service returns; when nil it returns wantID.
		svcErr error

		wantCode int
		// wantID is the id returned by the service and expected in the response body.
		wantID string
		// wantErr is the exact error message expected in the response body.
		wantErr string
		// wantErrContains is a fragment of the error message expected in the response body.
		wantErrContains string
		// wantAbsent is a fragment that must not appear anywhere in the response body.
		wantAbsent string
		// wantCalled is whether the request must reach the model service.
		wantCalled bool
		// checkReq inspects the request the service received.
		checkReq func(t *testing.T, req models.CreateModelRequest)
	}{
		{
			name:       "local model is accepted for background deployment",
			wantCode:   http.StatusAccepted,
			wantID:     localID,
			wantAbsent: "deployment_type", // the response carries only the id
			wantCalled: true,
			checkReq: func(t *testing.T, req models.CreateModelRequest) {
				assert.Equal(t, constants.DeploymentTypeLocal, req.DeploymentType)
				assert.Equal(t, "llm", req.ComponentType)
				assert.Equal(t, "granite-llm", req.Name)
				assert.Equal(t, "vllm-spyre", req.ProviderID)
				assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", req.Model)
				assert.Equal(t, "compact-4k", req.Profile)
				assert.Equal(t, "user-123", req.CreatedBy, "the creator comes from the token, not the body")
			},
		},
		{
			name: "remote model is created synchronously",
			mutate: func(r *models.CreateModelRequest) {
				r.DeploymentType = constants.DeploymentTypeRemote
				r.Name = "prod-watsonx"
				r.ProviderID = "watsonx"
				r.Model = "ibm/granite-4-h-small"
				r.Profile = ""
				r.Params = map[string]any{
					"api_base":   "https://us-south.ml.cloud.ibm.com",
					"project_id": "proj-1",
					"api_key":    "sk-test",
				}
			},
			wantCode:   http.StatusCreated,
			wantID:     remoteID,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.CreateModelRequest) {
				assert.Equal(t, constants.DeploymentTypeRemote, req.DeploymentType)
				assert.Equal(t, "prod-watsonx", req.Name)
				assert.Equal(t, "watsonx", req.ProviderID)
				assert.Equal(t, "ibm/granite-4-h-small", req.Model)
				assert.Equal(t, "sk-test", req.Params["api_key"])
			},
		},
		{
			name:     "missing token is unauthorized",
			noAuth:   true,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:            "malformed json is rejected",
			rawBody:         "{invalid json}",
			wantCode:        http.StatusBadRequest,
			wantErrContains: "Invalid request body",
		},
		{
			name:            "missing deployment_type is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.DeploymentType = "" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "DeploymentType",
		},
		{
			name:            "unknown deployment_type is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.DeploymentType = "cloud" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "DeploymentType",
		},
		{
			name:            "missing component_type is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.ComponentType = "" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "ComponentType",
		},
		{
			name:            "missing provider_id is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.ProviderID = "" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "ProviderID",
		},
		{
			name:            "missing model is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.Model = "" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "Model",
		},
		{
			name:            "name shorter than 3 characters is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.Name = "ab" },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "Name",
		},
		{
			name:            "name longer than 100 characters is rejected",
			mutate:          func(r *models.CreateModelRequest) { r.Name = strings.Repeat("a", 101) },
			wantCode:        http.StatusBadRequest,
			wantErrContains: "Name",
		},
		{
			name:       "name at the maximum length is accepted",
			mutate:     func(r *models.CreateModelRequest) { r.Name = strings.Repeat("a", 100) },
			wantCode:   http.StatusAccepted,
			wantID:     localID,
			wantCalled: true,
		},
		{
			name: "connectivity failure keeps its 422 and message",
			svcErr: &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: "Health probe failed for remote model endpoint",
			},
			mutate:     func(r *models.CreateModelRequest) { r.DeploymentType = constants.DeploymentTypeRemote },
			wantCode:   http.StatusUnprocessableEntity,
			wantErr:    "Health probe failed for remote model endpoint",
			wantCalled: true,
		},
		{
			name: "name conflict keeps its 409 and message",
			svcErr: &validators.ValidationError{
				Code:    http.StatusConflict,
				Message: "A model with name \"granite-llm\" already exists",
			},
			wantCode:   http.StatusConflict,
			wantErr:    "A model with name \"granite-llm\" already exists",
			wantCalled: true,
		},
		{
			name:       "wrapped validation error keeps its status",
			svcErr:     fmt.Errorf("wrapped: %w", &validators.ValidationError{Code: http.StatusNotFound, Message: "worker not found"}),
			wantCode:   http.StatusNotFound,
			wantErr:    "worker not found",
			wantCalled: true,
		},
		{
			name:       "internal error is not leaked",
			svcErr:     errors.New("litellm returned status 500: secret upstream body"),
			mutate:     func(r *models.CreateModelRequest) { r.DeploymentType = constants.DeploymentTypeRemote },
			wantCode:   http.StatusInternalServerError,
			wantErr:    "Failed to create model",
			wantAbsent: "secret upstream body",
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				called bool
				gotReq models.CreateModelRequest
			)

			svc := &mockModelService{
				createModelFunc: func(_ context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
					called = true
					gotReq = req

					if tt.svcErr != nil {
						return nil, tt.svcErr
					}

					return &models.CreateModelResponse{ID: tt.wantID}, nil
				},
			}

			tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
			router := setupModelRouter(svc, tm)

			body := tt.rawBody
			if body == "" {
				payload := validModelPayload()
				if tt.mutate != nil {
					tt.mutate(&payload)
				}

				b, err := json.Marshal(payload)
				require.NoError(t, err)
				body = string(b)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/models", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			if !tt.noAuth {
				token, _, err := tm.GenerateAccessToken("user-123")
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+token)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Equal(t, tt.wantCalled, called, "whether the request reached the model service")

			if tt.wantID != "" {
				var resp models.CreateModelResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.wantID, resp.ID)
			}

			if tt.wantErr != "" || tt.wantErrContains != "" {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))

				if tt.wantErr != "" {
					assert.Equal(t, tt.wantErr, errResp.Error)
				}

				if tt.wantErrContains != "" {
					assert.Contains(t, errResp.Error, tt.wantErrContains)
				}
			}

			if tt.wantAbsent != "" {
				assert.NotContains(t, w.Body.String(), tt.wantAbsent)
			}

			if tt.checkReq != nil {
				tt.checkReq(t, gotReq)
			}
		})
	}
}

func TestCreateModelStatus(t *testing.T) {
	tests := []struct {
		name           string
		deploymentType constants.DeploymentType
		want           int
	}{
		{"local deployments continue in the background", constants.DeploymentTypeLocal, http.StatusAccepted},
		{"remote registrations complete before responding", constants.DeploymentTypeRemote, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, createModelStatus(tt.deploymentType))
		})
	}
}

func TestListModels(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		noAuth bool
		// svcErr is what the model service returns; when nil it returns an empty page.
		svcErr error

		wantCode        int
		wantErrContains string
		wantAbsent      string
		// wantCalled is whether the request must reach the model service.
		wantCalled bool
		checkReq   func(t *testing.T, req models.ListModelsRequest)
	}{
		{
			name:       "defaults are applied and the owner comes from the token",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, 1, req.Page)
				assert.Equal(t, 20, req.PageSize)
				assert.Equal(t, "user-123", req.CreatedBy)
				assert.Empty(t, req.DeploymentType)
			},
		},
		{
			name:       "all filters are passed to the service",
			query:      "?component_type=llm&deployment_type=local&provider_id=vllm-spyre&model=ibm-granite/granite&status=Running&page=2&page_size=50",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, "llm", req.ComponentType)
				assert.Equal(t, constants.DeploymentTypeLocal, req.DeploymentType)
				assert.Equal(t, []string{"vllm-spyre"}, req.ProviderIDs)
				assert.Equal(t, []string{"ibm-granite/granite"}, req.Models)
				assert.Equal(t, "Running", req.Status)
				assert.Equal(t, 2, req.Page)
				assert.Equal(t, 50, req.PageSize)
				assert.Equal(t, "user-123", req.CreatedBy)
			},
		},
		{
			name:       "repeated provider_id and model parameters are all passed",
			query:      "?provider_id=vllm-spyre&provider_id=watsonx&model=ibm/granite-4-h-small&model=ibm-granite/granite-3.3-8b-instruct",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, []string{"vllm-spyre", "watsonx"}, req.ProviderIDs)
				assert.Equal(t, []string{"ibm/granite-4-h-small", "ibm-granite/granite-3.3-8b-instruct"}, req.Models)
			},
		},
		{
			name:       "empty provider_id and model values are not filters",
			query:      "?provider_id=&model=&model=ibm/granite-4-h-small",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Empty(t, req.ProviderIDs)
				assert.Equal(t, []string{"ibm/granite-4-h-small"}, req.Models)
			},
		},
		{
			name:       "a created_by query parameter cannot override the token",
			query:      "?created_by=someone-else",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, "user-123", req.CreatedBy)
			},
		},
		{
			name:       "remote deployment type is accepted",
			query:      "?deployment_type=remote",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, constants.DeploymentTypeRemote, req.DeploymentType)
			},
		},
		{
			name:     "missing token is unauthorized and never reaches the service",
			noAuth:   true,
			wantCode: http.StatusUnauthorized,
		},
		{
			// Validating the deployment type is the service's job; the handler only forwards it.
			name:       "deployment_type is forwarded for the service to validate",
			query:      "?deployment_type=cloud",
			wantCode:   http.StatusOK,
			wantCalled: true,
			checkReq: func(t *testing.T, req models.ListModelsRequest) {
				assert.Equal(t, constants.DeploymentType("cloud"), req.DeploymentType)
			},
		},
		{
			name:            "page_size above the maximum is rejected",
			query:           "?page_size=101",
			wantCode:        http.StatusBadRequest,
			wantErrContains: "page_size",
		},
		{
			name:            "negative page is rejected",
			query:           "?page=-1",
			wantCode:        http.StatusBadRequest,
			wantErrContains: "page",
		},
		{
			name:       "validation error keeps its status",
			svcErr:     &validators.ValidationError{Code: http.StatusBadRequest, Message: "bad filter"},
			wantCode:   http.StatusBadRequest,
			wantCalled: true,

			wantErrContains: "bad filter",
		},
		{
			name:            "internal error is not leaked",
			svcErr:          errors.New("pq: connection refused to 10.0.0.5"),
			wantCode:        http.StatusInternalServerError,
			wantErrContains: "Failed to list models",
			wantAbsent:      "10.0.0.5",
			wantCalled:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				called bool
				gotReq models.ListModelsRequest
			)

			svc := &mockModelService{
				listModelsFunc: func(_ context.Context, req models.ListModelsRequest) (*models.ModelListResponse, error) {
					called = true
					gotReq = req

					if tt.svcErr != nil {
						return nil, tt.svcErr
					}

					return &models.ModelListResponse{Data: []models.ModelListItem{}}, nil
				},
			}

			tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
			router := setupModelRouter(svc, tm)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/models"+tt.query, nil)

			if !tt.noAuth {
				token, _, err := tm.GenerateAccessToken("user-123")
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+token)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Equal(t, tt.wantCalled, called, "whether the request reached the model service")

			if tt.wantErrContains != "" {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
				assert.Contains(t, errResp.Error, tt.wantErrContains)
			}

			if tt.wantAbsent != "" {
				assert.NotContains(t, w.Body.String(), tt.wantAbsent)
			}

			if tt.checkReq != nil {
				tt.checkReq(t, gotReq)
			}
		})
	}
}

func TestListModels_ResponseShape(t *testing.T) {
	svc := &mockModelService{
		listModelsFunc: func(context.Context, models.ListModelsRequest) (*models.ModelListResponse, error) {
			return &models.ModelListResponse{
				Data: []models.ModelListItem{{ModelBase: models.ModelBase{
					ID:             "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f",
					DeploymentType: constants.DeploymentTypeRemote,
					Name:           "prod-watsonx",
					Provider:       types.ProviderInfo{ID: "watsonx", Name: "Watsonx"},
					Model:          "ibm/granite-4-h-small",
					Status:         "connected",
				}}},
			}, nil
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	router := setupModelRouter(svc, tm)

	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var raw struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.Len(t, raw.Data, 1)

	item := raw.Data[0]
	assert.Equal(t, "remote", item["deployment_type"])
	// Per the LLD, worker is present and null for a remote model.
	assert.Contains(t, item, "worker")
	assert.Nil(t, item["worker"])
	assert.NotContains(t, item, "profile", "profile is not part of the list response")
	assert.Contains(t, item, "connected_services")
}

func TestGetModelByID(t *testing.T) {
	const modelID = "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f"

	tests := []struct {
		name            string
		path            string
		noAuth          bool
		svcErr          error
		wantCode        int
		wantCalled      bool
		wantErrContains string
		wantAbsent      string
	}{
		{name: "found", path: modelID, wantCode: http.StatusOK, wantCalled: true},
		{name: "malformed id", path: "not-a-uuid", wantCode: http.StatusBadRequest, wantErrContains: "Invalid model ID"},
		{name: "no token", path: modelID, noAuth: true, wantCode: http.StatusUnauthorized},
		{
			name:            "not found",
			path:            modelID,
			svcErr:          validators.NotFound("model not found"),
			wantCode:        http.StatusNotFound,
			wantCalled:      true,
			wantErrContains: "model not found",
		},
		{
			name:            "not the owner",
			path:            modelID,
			svcErr:          validators.Forbidden("user does not own this model"),
			wantCode:        http.StatusForbidden,
			wantCalled:      true,
			wantErrContains: "does not own",
		},
		{
			name:            "internal error is not leaked",
			path:            modelID,
			svcErr:          errors.New("pq: connection refused to 10.0.0.5"),
			wantCode:        http.StatusInternalServerError,
			wantCalled:      true,
			wantErrContains: "Failed to get model",
			wantAbsent:      "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				called   bool
				gotID    uuid.UUID
				gotOwner string
			)

			svc := &mockModelService{
				getModelFunc: func(_ context.Context, id uuid.UUID, createdBy string) (*models.ModelDetail, error) {
					called = true
					gotID = id
					gotOwner = createdBy

					if tt.svcErr != nil {
						return nil, tt.svcErr
					}

					return &models.ModelDetail{ModelBase: models.ModelBase{ID: id.String()}}, nil
				},
			}

			tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
			router := setupModelRouter(svc, tm)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+tt.path, nil)

			if !tt.noAuth {
				token, _, err := tm.GenerateAccessToken("user-123")
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+token)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Equal(t, tt.wantCalled, called, "whether the request reached the model service")

			if tt.wantCalled {
				assert.Equal(t, modelID, gotID.String())
				assert.Equal(t, "user-123", gotOwner, "the owner comes from the token, not the request")
			}

			if tt.wantErrContains != "" {
				var errResp ErrorResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
				assert.Contains(t, errResp.Error, tt.wantErrContains)
			}

			if tt.wantAbsent != "" {
				assert.NotContains(t, w.Body.String(), tt.wantAbsent)
			}
		})
	}
}

func TestGetModelByID_ResponseShape(t *testing.T) {
	svc := &mockModelService{
		getModelFunc: func(_ context.Context, id uuid.UUID, _ string) (*models.ModelDetail, error) {
			return &models.ModelDetail{
				ModelBase: models.ModelBase{
					ID:             id.String(),
					DeploymentType: constants.DeploymentTypeRemote,
					Name:           "prod-watsonx",
					Provider:       types.ProviderInfo{ID: "watsonx", Name: "Watsonx"},
					Model:          "ibm/granite-4-h-small",
					Status:         "connected",
				},
				Endpoints:    []models.ModelEndpoint{{Type: "external", URL: "https://litellm.example.com"}},
				Applications: []models.ModelApplication{{ID: "a1", Name: "chatbot"}},
			}, nil
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	router := setupModelRouter(svc, tm)

	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	assert.Equal(t, "remote", raw["deployment_type"])
	assert.Contains(t, raw, "worker")
	assert.Nil(t, raw["worker"])
	assert.Contains(t, raw, "endpoints")
	assert.NotContains(t, raw, "profile")
	assert.Len(t, raw["applications"], 1)
	assert.Contains(t, raw, "connected_services")
}
