package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockModelService struct {
	createModelFunc func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error)
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

	return router
}

// validModelPayload is a request that passes the handler's binding rules.
func validModelPayload() models.CreateModelRequest {
	return models.CreateModelRequest{
		DeploymentType: models.DeploymentTypeLocal,
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
				assert.Equal(t, models.DeploymentTypeLocal, req.DeploymentType)
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
				r.DeploymentType = models.DeploymentTypeRemote
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
				assert.Equal(t, models.DeploymentTypeRemote, req.DeploymentType)
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
			mutate:     func(r *models.CreateModelRequest) { r.DeploymentType = models.DeploymentTypeRemote },
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
			mutate:     func(r *models.CreateModelRequest) { r.DeploymentType = models.DeploymentTypeRemote },
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
		deploymentType models.DeploymentType
		want           int
	}{
		{"local deployments continue in the background", models.DeploymentTypeLocal, http.StatusAccepted},
		{"remote registrations complete before responding", models.DeploymentTypeRemote, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, createModelStatus(tt.deploymentType))
		})
	}
}
