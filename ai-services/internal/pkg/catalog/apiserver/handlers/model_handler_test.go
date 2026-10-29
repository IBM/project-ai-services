package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	return &models.CreateModelResponse{
		ID: "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f",
	}, nil
}

func setupModelRouter(svc repository.ModelServiceInterface, tm *auth.TokenManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authMw := middleware.AuthMiddleware(tm, &repository.NoopTokenBlacklist{})
	h := NewModelHandler(svc)
	router.POST("/api/v1/models", authMw, h.CreateModel)

	return router
}

func TestCreateModel_Local_Success(t *testing.T) {
	mockSvc := &mockModelService{
		createModelFunc: func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
			assert.Equal(t, "local", req.DeploymentType)
			assert.Equal(t, "llm", req.ComponentType)
			assert.Equal(t, "granite-llm", req.Name)
			assert.Equal(t, "vllm-spyre", req.ProviderID)
			assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", req.Model)
			assert.Equal(t, "compact-4k", req.Profile)
			assert.Equal(t, "user-123", req.CreatedBy)

			return &models.CreateModelResponse{
				ID: "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f",
			}, nil
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	router := setupModelRouter(mockSvc, tm)

	payload := models.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "granite-llm",
		ProviderID:     "vllm-spyre",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		Profile:        "compact-4k",
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp models.CreateModelResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f", resp.ID)
}

func TestCreateModel_Remote_Success(t *testing.T) {
	mockSvc := &mockModelService{
		createModelFunc: func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
			assert.Equal(t, "remote", req.DeploymentType)
			assert.Equal(t, "llm", req.ComponentType)
			assert.Equal(t, "prod-watsonx", req.Name)
			assert.Equal(t, "watsonx", req.ProviderID)
			assert.Equal(t, "ibm/granite-4-h-small", req.Model)

			return &models.CreateModelResponse{
				ID: "c1d2e3f4-a5b6-7890-cdef-123456789abc",
			}, nil
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	router := setupModelRouter(mockSvc, tm)

	payload := models.CreateModelRequest{
		DeploymentType: "remote",
		ComponentType:  "llm",
		Name:           "prod-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-4-h-small",
		Params: map[string]any{
			"api_base":   "https://us-south.ml.cloud.ibm.com",
			"project_id": "proj-1",
			"api_key":    "sk-test",
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp models.CreateModelResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "c1d2e3f4-a5b6-7890-cdef-123456789abc", resp.ID)
}

func TestCreateModel_Unauthorized(t *testing.T) {
	mockSvc := &mockModelService{}
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	router := setupModelRouter(mockSvc, tm)

	payload := models.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "test-model",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateModel_InvalidBody(t *testing.T) {
	mockSvc := &mockModelService{}
	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	router := setupModelRouter(mockSvc, tm)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewBufferString("{invalid json}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateModel_ConnectivityError(t *testing.T) {
	mockSvc := &mockModelService{
		createModelFunc: func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
			return nil, &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: "Health probe failed for remote model endpoint",
			}
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	router := setupModelRouter(mockSvc, tm)

	payload := models.CreateModelRequest{
		DeploymentType: "remote",
		ComponentType:  "llm",
		Name:           "remote-watsonx",
		ProviderID:     "watsonx",
		Model:          "ibm/granite-3-8b-instruct",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var errResp ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "Health probe failed for remote model endpoint", errResp.Error)
}

func TestCreateModel_ValidationError(t *testing.T) {
	mockSvc := &mockModelService{
		createModelFunc: func(ctx context.Context, req models.CreateModelRequest) (*models.CreateModelResponse, error) {
			return nil, &validators.ValidationError{
				Code:    http.StatusConflict,
				Message: "A model with name \"granite-llm\" already exists",
			}
		},
	}

	tm := auth.NewTokenManager("test-secret-key-32-bytes-auth1", 15*time.Minute, 24*time.Hour)
	token, _, err := tm.GenerateAccessToken("user-123")
	require.NoError(t, err)

	router := setupModelRouter(mockSvc, tm)

	payload := models.CreateModelRequest{
		DeploymentType: "local",
		ComponentType:  "llm",
		Name:           "granite-llm",
		ProviderID:     "vllm-spyre",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}
