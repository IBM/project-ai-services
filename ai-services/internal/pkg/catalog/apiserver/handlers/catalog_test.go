package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
	assert.Equal(t, "Digital assistants", arch.Name)
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
				var images []string
				require.NoError(t, json.Unmarshal(body, &images))
				assert.NotEmpty(t, images)
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
				var images []string
				require.NoError(t, json.Unmarshal(body, &images))
				assert.NotEmpty(t, images)
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

func TestGetServiceModels(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/services/:id/models", handler.GetServiceModels)

	tests := []struct {
		name           string
		serviceID      string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Returns models array for existing service",
			serviceID:      "chat",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var models []string
				require.NoError(t, json.Unmarshal(body, &models))
				assert.NotNil(t, models)
			},
		},
		{
			name:           "Returns 404 for unknown service",
			serviceID:      "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody: func(t *testing.T, body []byte) {
				var errResp map[string]string
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Contains(t, errResp["error"], "nonexistent")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/services/"+tt.serviceID+"/models", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func TestGetArchitectureModels(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/architectures/:id/models", handler.GetArchitectureModels)

	tests := []struct {
		name           string
		archID         string
		expectedStatus int
		validateBody   func(t *testing.T, body []byte)
	}{
		{
			name:           "Returns models array for rag architecture",
			archID:         "rag",
			expectedStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				var models []string
				require.NoError(t, json.Unmarshal(body, &models))
				assert.NotNil(t, models)
			},
		},
		{
			name:           "Returns 404 for unknown architecture",
			archID:         "nonexistent",
			expectedStatus: http.StatusNotFound,
			validateBody: func(t *testing.T, body []byte) {
				var errResp map[string]string
				require.NoError(t, json.Unmarshal(body, &errResp))
				assert.Contains(t, errResp["error"], "nonexistent")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/architectures/"+tt.archID+"/models", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.validateBody != nil {
				tt.validateBody(t, w.Body.Bytes())
			}
		})
	}
}

func TestListCatalogModels(t *testing.T) {
	router := setupTestRouter()
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/catalog/models", handler.ListModels)

	get := func(url string) []types.ModelCatalogEntry {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var list []types.ModelCatalogEntry
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))

		return list
	}

	list := get("/api/v1/catalog/models")
	byID := map[string]types.ModelCatalogEntry{}
	ids := make([]string, 0, len(list))
	for _, m := range list {
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	assert.True(t, slices.IsSorted(ids), "models are sorted by id")

	t.Run("entry shape", func(t *testing.T) {
		m := byID["ibm-granite/granite-3.3-8b-instruct"]
		assert.Equal(t, "Granite 3.3 8B Instruct", m.Name)
		assert.Equal(t, "IBM", m.CertifiedBy)
		assert.NotEmpty(t, m.Description)
		assert.Equal(t, "llm", m.ComponentType.ID)
		assert.NotEmpty(t, m.ComponentType.Name)
		assert.NotEmpty(t, m.ComponentType.ShortName)
	})

	t.Run("providers are local first, then remote", func(t *testing.T) {
		m := byID["ibm-granite/granite-3.3-8b-instruct"]
		require.NotEmpty(t, m.Providers)
		seenRemote := false
		for _, pr := range m.Providers {
			assert.NotEmpty(t, pr.ID)
			assert.NotEmpty(t, pr.Name)
			if pr.DeploymentType == "remote" {
				seenRemote = true
			} else {
				assert.False(t, seenRemote, "local provider after a remote one: %+v", m.Providers)
			}
		}
	})

	t.Run("model of a component and a connector is listed once", func(t *testing.T) {
		count := 0
		for _, m := range list {
			if m.ID == "BAAI/bge-reranker-v2-m3" {
				count++
			}
		}
		assert.Equal(t, 1, count)
		assert.Equal(t, "reranker", byID["BAAI/bge-reranker-v2-m3"].ComponentType.ID)
	})

	t.Run("component_type filter", func(t *testing.T) {
		filtered := get("/api/v1/catalog/models?component_type=reranker")
		require.NotEmpty(t, filtered)
		for _, m := range filtered {
			assert.Equal(t, "reranker", m.ComponentType.ID)
		}

		assert.Empty(t, get("/api/v1/catalog/models?component_type=nope"))
	})
}

func TestGetCatalogModel(t *testing.T) {
	router := setupTestRouter()
	router.UseRawPath = true
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/catalog/models/:id", handler.GetCatalogModel)

	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		router.ServeHTTP(w, req)

		return w
	}

	t.Run("model id containing an encoded slash", func(t *testing.T) {
		w := get("/api/v1/catalog/models/ibm-granite%2Fgranite-3.3-8b-instruct")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var m types.ModelDetail
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
		assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", m.ID)
		assert.Equal(t, "Granite 3.3 8B Instruct", m.Name)
		assert.Equal(t, "llm", m.ComponentType.ID)
		assert.NotEmpty(t, m.ComponentType.Name)
		assert.NotEmpty(t, m.ComponentType.ShortName)
		assert.Equal(t, "IBM", m.CertifiedBy)
		assert.Equal(t, "Apache-2.0", m.License)
		assert.NotEmpty(t, m.Description)
		assert.NotEmpty(t, m.About, "the get-by-id view includes about")
		assert.NotEmpty(t, m.Providers, "the get-by-id view includes providers")
	})

	// Same providers, in the same order, as the list entry: both come from one collector.
	t.Run("providers match the list entry", func(t *testing.T) {
		listRouter := setupTestRouter()
		listRouter.GET("/api/v1/catalog/models", handler.ListModels)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/catalog/models", nil)
		listRouter.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var list []types.ModelCatalogEntry
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))

		for _, entry := range list {
			w := get("/api/v1/catalog/models/" + url.PathEscape(entry.ID))
			require.Equal(t, http.StatusOK, w.Code, entry.ID)

			var m types.ModelDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
			assert.Equal(t, entry, m.ModelCatalogEntry, entry.ID)
		}
	})

	t.Run("model listed by several providers is stable", func(t *testing.T) {
		for range 20 {
			w := get("/api/v1/catalog/models/BAAI%2Fbge-reranker-v2-m3")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var m types.ModelDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
			require.Equal(t, "MIT", m.License)
			require.Equal(t, "BAAI", m.CertifiedBy)
			require.Len(t, m.Providers, 3)
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		w := get("/api/v1/catalog/models/no%2Fsuch-model")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestGetModelDeployOptions(t *testing.T) {
	router := setupTestRouter()
	router.UseRawPath = true
	handler := newTestCatalogHandler(t)
	router.GET("/api/v1/catalog/models/:id/deploy-options", handler.GetModelDeployOptions)

	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		router.ServeHTTP(w, req)

		return w
	}

	t.Run("model id containing an encoded slash", func(t *testing.T) {
		w := get("/api/v1/catalog/models/ibm-granite%2Fgranite-3.3-8b-instruct/deploy-options?runtime=podman")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var opts types.ModelDeployOptions
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &opts))
		assert.Equal(t, "ibm-granite/granite-3.3-8b-instruct", opts.ID)
		assert.NotEmpty(t, opts.Providers)
	})

	t.Run("provider_id filter", func(t *testing.T) {
		w := get("/api/v1/catalog/models/ibm-granite%2Fgranite-3.3-8b-instruct/deploy-options?runtime=podman&provider_id=vllm-cpu")
		require.Equal(t, http.StatusOK, w.Code)

		var opts types.ModelDeployOptions
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &opts))
		require.Len(t, opts.Providers, 1)
		assert.Equal(t, "vllm-cpu", opts.Providers[0].ID)
	})

	t.Run("invalid runtime", func(t *testing.T) {
		w := get("/api/v1/catalog/models/ibm-granite%2Fgranite-3.3-8b-instruct/deploy-options?runtime=docker")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unknown model", func(t *testing.T) {
		w := get("/api/v1/catalog/models/no%2Fsuch-model/deploy-options?runtime=podman")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("unencoded slash does not match the route", func(t *testing.T) {
		w := get("/api/v1/catalog/models/ibm-granite/granite-3.3-8b-instruct/deploy-options?runtime=podman")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// Made with Bob
