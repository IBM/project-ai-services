package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Initialize runtime factory for tests
	if vars.RuntimeFactory == nil {
		vars.RuntimeFactory = runtime.NewRuntimeFactory(runtimeTypes.RuntimeTypePodman)
	}

	return router
}

// newTestCatalogHandler creates a CatalogHandler backed by embedded-only catalog items
// (no bundle DB) — sufficient for unit tests that exercise built-in catalog entries.
func newTestCatalogHandler(t *testing.T) *CatalogHandler {
	t.Helper()
	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)
	return NewCatalogHandler(provider)
}

func TestListArchitectures(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/architectures", handler.ListArchitectures)

	tests := []struct {
		name           string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully list architectures",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var architectures []types.ArchitectureSummary
				err := json.Unmarshal(body, &architectures)
				require.NoError(t, err)

				// Should have at least one architecture (rag)
				assert.NotEmpty(t, architectures)

				// Verify structure of first architecture
				if len(architectures) > 0 {
					arch := architectures[0]
					assert.NotEmpty(t, arch.ID)
					assert.NotEmpty(t, arch.Name)
					assert.NotEmpty(t, arch.Description)
					assert.NotEmpty(t, arch.CertifiedBy)
					assert.NotEmpty(t, arch.Services)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/architectures", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func TestGetArchitecture(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/architectures/:id", handler.GetArchitectureDetails)

	tests := []struct {
		name           string
		archID         string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully get rag architecture",
			archID:         "rag",
			expectedStatus: http.StatusOK,
			validateBody:   validateRagArchitecture,
		},
		{
			name:           "Architecture not found",
			archID:         "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateArchitectureNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/architectures/"+tt.archID, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func validateRagArchitecture(t *testing.T, body []byte) {
	var arch types.Architecture
	err := json.Unmarshal(body, &arch)
	require.NoError(t, err)

	assert.Equal(t, "rag", arch.ID)
	assert.Equal(t, "Digital Assistants", arch.Name)
	assert.NotEmpty(t, arch.Description)
	assert.Equal(t, "1.0.0", arch.Version)
	assert.Equal(t, "architecture", arch.Type)
	assert.Equal(t, "IBM", arch.CertifiedBy)
	assert.Contains(t, arch.Runtimes, "podman")

	assert.NotEmpty(t, arch.Services)
	serviceIDs := make(map[string]bool)
	for _, svc := range arch.Services {
		serviceIDs[svc.ID] = true
	}
	assert.True(t, serviceIDs["chat"])
	assert.True(t, serviceIDs["digitize"])
	assert.True(t, serviceIDs["similarity"])
}

func validateArchitectureNotFound(t *testing.T, body []byte) {
	var errResp map[string]string
	err := json.Unmarshal(body, &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "not found")
}

func TestListServices(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/services", handler.ListServices)

	tests := []struct {
		name           string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully list services",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var services []types.ServiceSummary
				err := json.Unmarshal(body, &services)
				require.NoError(t, err)

				// Should have deployable services
				assert.NotEmpty(t, services)
	
				serviceIDs := make(map[string]bool)
				for _, svc := range services {
					serviceIDs[svc.ID] = true
					// Verify every entry has at minimum an ID and name
					assert.NotEmpty(t, svc.ID)
					assert.NotEmpty(t, svc.Name)
					assert.NotEmpty(t, svc.Description)
				}
	
				// Should include deployable services
				assert.True(t, serviceIDs["chat"])
				assert.True(t, serviceIDs["digitize"])
				assert.True(t, serviceIDs["summarize"])
	
				// Verify architectures for services known to have them
				for _, svc := range services {
					if svc.ID == "chat" || svc.ID == "digitize" || svc.ID == "similarity" {
						assert.NotEmpty(t, svc.Architectures, "service %q should have architectures", svc.ID)
					}
				}
	
				// Components (opensearch, embedding, instruct, reranker) are not services
				assert.False(t, serviceIDs["opensearch"])
				assert.False(t, serviceIDs["embedding"])
				assert.False(t, serviceIDs["instruct"])
				assert.False(t, serviceIDs["reranker"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/services", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func TestGetService(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/services/:id", handler.GetServiceDetails)

	tests := []struct {
		name           string
		serviceID      string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully get chat service",
			serviceID:      "chat",
			expectedStatus: http.StatusOK,
			validateBody:   validateChatService,
		},
		{
			name:           "Service not found",
			serviceID:      "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/services/"+tt.serviceID, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func validateChatService(t *testing.T, body []byte) {
	var svc types.Service
	err := json.Unmarshal(body, &svc)
	require.NoError(t, err)

	assert.Equal(t, "chat", svc.ID)
	assert.Equal(t, "Question and answer", svc.Name)
	assert.Equal(t, "service", svc.Type)
	assert.Equal(t, "IBM", svc.CertifiedBy)
	assert.Contains(t, svc.Architectures, "rag")

	assert.NotEmpty(t, svc.Dependencies)
	depIDs := make(map[string]bool)
	for _, dep := range svc.Dependencies {
		depIDs[dep.ID] = true
	}
	assert.True(t, depIDs["vector_store"])
	assert.True(t, depIDs["embedding"])
	assert.True(t, depIDs["llm"])
	assert.NotEmpty(t, svc.Architectures)
}

func validateServiceNotFound(t *testing.T, body []byte) {
	var errResp map[string]string
	err := json.Unmarshal(body, &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "not found")
}

// validateNotFoundBody asserts the response body contains a JSON error field with "not found".
func validateNotFoundBody(t *testing.T, body []byte) {
	t.Helper()

	var errResp map[string]string
	err := json.Unmarshal(body, &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "not found")
}

func TestGetServiceImages(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/services/:id/images", handler.GetServiceImages)

	tests := []struct {
		name           string
		serviceID      string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully get images for chat service",
			serviceID:      "chat",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var resp ImagesResponse
				err := json.Unmarshal(body, &resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.Images)
			},
		},
		{
			name:           "Architecture id returns 404 — wrong endpoint",
			serviceID:      "rag",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateNotFoundBody,
		},
		{
			name:           "Unknown id returns 404",
			serviceID:      "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateNotFoundBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/services/"+tt.serviceID+"/images", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func TestGetArchitectureImages(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/architectures/:id/images", handler.GetArchitectureImages)

	tests := []struct {
		name           string
		archID         string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully get images for rag architecture",
			archID:         "rag",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var resp ImagesResponse
				err := json.Unmarshal(body, &resp)
				require.NoError(t, err)
				assert.NotEmpty(t, resp.Images)
			},
		},
		{
			name:           "Service id returns 404 — wrong endpoint",
			archID:         "chat",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateNotFoundBody,
		},
		{
			name:           "Unknown id returns 404",
			archID:         "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody:   validateNotFoundBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/v1/architectures/"+tt.archID+"/images", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

// Made with Bob
