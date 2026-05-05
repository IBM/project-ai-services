package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

func TestListArchitectures(t *testing.T) {
	router := setupTestRouter()
	handler := NewCatalogHandler()
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
	handler := NewCatalogHandler()
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
			validateBody: func(t *testing.T, body []byte) {
				var arch types.Architecture
				err := json.Unmarshal(body, &arch)
				require.NoError(t, err)

				assert.Equal(t, "rag", arch.ID)
				assert.Equal(t, "Digital Assistant", arch.Name)
				assert.NotEmpty(t, arch.Description)
				assert.Equal(t, "1.0.0", arch.Version)
				assert.Equal(t, "architecture", arch.Type)
				assert.Equal(t, "IBM", arch.CertifiedBy)
				assert.Contains(t, arch.Runtimes, "podman")

				// Verify services
				assert.NotEmpty(t, arch.Services)
				serviceIDs := make(map[string]bool)
				for _, svc := range arch.Services {
					serviceIDs[svc.ID] = true
				}
				assert.True(t, serviceIDs["chat"])
				assert.True(t, serviceIDs["digitize"])
				assert.True(t, serviceIDs["summarize"])
			},
		},
		{
			name:           "Architecture not found",
			archID:         "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody: func(t *testing.T, body []byte) {
				var errResp map[string]string
				err := json.Unmarshal(body, &errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp["error"], "not found")
			},
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

func TestListServices(t *testing.T) {
	router := setupTestRouter()
	handler := NewCatalogHandler()
	router.GET("/api/v1/services", handler.ListServices)

	tests := []struct {
		name           string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Successfully list services (excludes dependency-only)",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var services []types.ServiceSummary
				err := json.Unmarshal(body, &services)
				require.NoError(t, err)

				// Should have deployable services only
				assert.NotEmpty(t, services)

				// Verify dependency-only services are excluded
				serviceIDs := make(map[string]bool)
				for _, svc := range services {
					serviceIDs[svc.ID] = true
					// Verify structure
					assert.NotEmpty(t, svc.ID)
					assert.NotEmpty(t, svc.Name)
					assert.NotEmpty(t, svc.Description)
					assert.NotEmpty(t, svc.CertifiedBy)
					assert.NotEmpty(t, svc.Architectures)
				}

				// Should include deployable services
				assert.True(t, serviceIDs["chat"])
				assert.True(t, serviceIDs["digitize"])
				assert.True(t, serviceIDs["summarize"])

				// Should NOT include dependency-only services
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
	handler := NewCatalogHandler()
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
			validateBody: func(t *testing.T, body []byte) {
				var svc types.Service
				err := json.Unmarshal(body, &svc)
				require.NoError(t, err)

				assert.Equal(t, "chat", svc.ID)
				assert.Equal(t, "Question and Answer", svc.Name)
				assert.Equal(t, "service", svc.Type)
				assert.Equal(t, "IBM", svc.CertifiedBy)
				assert.Contains(t, svc.Architectures, "rag")

				// Verify dependencies
				assert.NotEmpty(t, svc.Dependencies)
				depIDs := make(map[string]bool)
				for _, dep := range svc.Dependencies {
					depIDs[dep.ID] = true
				}
				assert.True(t, depIDs["opensearch"])
				assert.True(t, depIDs["embedding"])
				assert.True(t, depIDs["instruct"])
				assert.True(t, depIDs["reranker"])

				assert.NotEmpty(t, svc.Architectures)
			},
		},
		{
			name:           "Successfully get dependency-only service (opensearch)",
			serviceID:      "opensearch",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var svc types.Service
				err := json.Unmarshal(body, &svc)
				require.NoError(t, err)

				assert.Equal(t, "opensearch", svc.ID)
				assert.Equal(t, "OpenSearch", svc.Name)
				assert.True(t, svc.DependencyOnly)
				assert.Empty(t, svc.Dependencies) // Dependency-only services have no dependencies
			},
		},
		{
			name:           "Service not found",
			serviceID:      "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody: func(t *testing.T, body []byte) {
				var errResp map[string]string
				err := json.Unmarshal(body, &errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp["error"], "not found")
			},
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

// Made with Bob
