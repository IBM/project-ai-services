package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockWorkerRepo struct {
	workers map[string]*models.Worker
	byID    map[uuid.UUID]*models.Worker
}

func newMockWorkerRepo() *mockWorkerRepo {
	return &mockWorkerRepo{
		workers: make(map[string]*models.Worker),
		byID:    make(map[uuid.UUID]*models.Worker),
	}
}

func (r *mockWorkerRepo) Upsert(_ context.Context, w *models.Worker) error {
	if existing, ok := r.workers[w.Name]; ok {
		w.ID = existing.ID
	} else {
		w.ID = uuid.New()
	}
	w.RegisteredAt = time.Now()
	w.UpdatedAt = time.Now()
	cp := *w
	r.workers[w.Name] = &cp
	r.byID[w.ID] = &cp
	return nil
}

func (r *mockWorkerRepo) Update(_ context.Context, id uuid.UUID, u repository.WorkerUpdate) error {
	w, ok := r.byID[id]
	if !ok {
		return nil
	}
	if u.Status != nil {
		w.Status = *u.Status
	}
	if u.LastHeartbeat != nil {
		w.LastHeartbeat = u.LastHeartbeat
	}
	return nil
}

func (r *mockWorkerRepo) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	w, ok := r.byID[id]
	if !ok {
		return false, nil
	}
	delete(r.workers, w.Name)
	delete(r.byID, id)
	return true, nil
}

func (r *mockWorkerRepo) GetCount(_ context.Context, req *repository.ListWorkersRequest) (int, error) {
	workers, err := r.GetAll(context.Background(), &repository.ListWorkersRequest{Search: req.Search})
	if err != nil {
		return 0, err
	}
	return len(workers), nil
}

func (r *mockWorkerRepo) GetAll(_ context.Context, req *repository.ListWorkersRequest) ([]models.Worker, error) {
	out := make([]models.Worker, 0, len(r.workers))
	for _, w := range r.workers {
		if req != nil && req.Search != "" && !strings.Contains(strings.ToLower(w.Name), strings.ToLower(req.Search)) {
			continue
		}
		out = append(out, *w)
	}
	if req != nil && (req.Offset > 0 || req.Limit > 0) {
		start := req.Offset
		if start > len(out) {
			return []models.Worker{}, nil
		}
		end := len(out)
		if req.Limit > 0 && start+req.Limit < end {
			end = start + req.Limit
		}
		return out[start:end], nil
	}
	return out, nil
}

func (r *mockWorkerRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Worker, error) {
	w, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *mockWorkerRepo) GetByName(_ context.Context, name string) (*models.Worker, error) {
	w, ok := r.workers[name]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *mockWorkerRepo) GetApplicationIDsByWorkerIDs(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
}

func setupWorkerRouter(handler *WorkerHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/workers", handler.CreateWorker)
	router.GET("/api/v1/workers", handler.ListWorkers)
	router.GET("/api/v1/workers/:id", handler.GetWorker)
	router.DELETE("/api/v1/workers/:id", handler.DeleteWorker)
	return router
}

func TestWorkerHandler_CreateWorker(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")

	tests := []struct {
		name           string
		setup          func(reg *registry.Registry, repo *mockWorkerRepo)
		body           string
		wantStatusCode int
		checkBody      func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:           "success - new worker registered",
			body:           `{"worker_name": "worker-1"}`,
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp createWorkerResp
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "worker-1", resp.WorkerName)
				assert.NotEmpty(t, resp.Token)
			},
		},
		{
			name: "409 conflict - worker is already registered and ready",
			setup: func(reg *registry.Registry, repo *mockWorkerRepo) {
				_, err := reg.Preregister(context.Background(), "worker-ready")
				require.NoError(t, err)
				_, err = reg.Register(context.Background(), "worker-ready", "podman", nil)
				require.NoError(t, err)
			},
			body:           `{"worker_name": "worker-ready"}`,
			wantStatusCode: http.StatusConflict,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Contains(t, resp["error"], "already registered and ready")
			},
		},
		{
			name: "success - re-registration allowed for disconnected worker",
			setup: func(reg *registry.Registry, repo *mockWorkerRepo) {
				_, err := reg.Preregister(context.Background(), "worker-disc")
				require.NoError(t, err)
				_, err = reg.Register(context.Background(), "worker-disc", "podman", nil)
				require.NoError(t, err)
				reg.Disconnect(context.Background(), "worker-disc")
			},
			body:           `{"worker_name": "worker-disc"}`,
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp createWorkerResp
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "worker-disc", resp.WorkerName)
				assert.NotEmpty(t, resp.Token)
			},
		},
		{
			name: "success - re-registration allowed for pending worker",
			setup: func(reg *registry.Registry, repo *mockWorkerRepo) {
				_, err := reg.Preregister(context.Background(), "worker-pending")
				require.NoError(t, err)
			},
			body:           `{"worker_name": "worker-pending"}`,
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp createWorkerResp
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "worker-pending", resp.WorkerName)
				assert.NotEmpty(t, resp.Token)
			},
		},
		{
			name:           "400 bad request - invalid payload",
			body:           `invalid json`,
			wantStatusCode: http.StatusBadRequest,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "invalid payload", resp["error"])
			},
		},
		{
			name:           "400 bad request - name too short",
			body:           `{"worker_name": "ab"}`,
			wantStatusCode: http.StatusBadRequest,
			checkBody: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Contains(t, resp["error"], "worker name must be between 3 and 64 characters")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockWorkerRepo()
			reg := registry.New(repo)
			if tt.setup != nil {
				tt.setup(reg, repo)
			}
			handler := NewWorkerHandler(reg, repo, types.RuntimeTypePodman, 9191)
			router := setupWorkerRouter(handler)

			req, err := http.NewRequest(http.MethodPost, "/api/v1/workers", bytes.NewBufferString(tt.body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w)
			}
		})
	}
}

func TestWorkerHandler_ListWorkers_Pagination(t *testing.T) {
	repo := newMockWorkerRepo()
	for i := 1; i <= 5; i++ {
		w := &models.Worker{
			ID:          uuid.New(),
			Name:        fmt.Sprintf("worker-%d", i),
			RuntimeType: models.WorkerRuntimeTypePodman,
			Status:      models.WorkerStatusReady,
		}
		_ = repo.Upsert(context.Background(), w)
	}

	reg := registry.New(repo)
	handler := NewWorkerHandler(reg, repo, types.RuntimeTypePodman, 9191)
	router := setupWorkerRouter(handler)

	tests := []struct {
		name           string
		query          string
		wantStatusCode int
		wantCount      int
		wantPage       int
		wantPageSize   int
		wantTotalItems int
		wantTotalPages int
		wantHasNext    bool
		wantHasPrev    bool
	}{
		{
			name:           "default pagination",
			query:          "",
			wantStatusCode: http.StatusOK,
			wantCount:      5,
			wantPage:       1,
			wantPageSize:   20,
			wantTotalItems: 5,
			wantTotalPages: 1,
		},
		{
			name:           "custom page size and page",
			query:          "?page=1&page_size=2",
			wantStatusCode: http.StatusOK,
			wantCount:      2,
			wantPage:       1,
			wantPageSize:   2,
			wantTotalItems: 5,
			wantTotalPages: 3,
			wantHasNext:    true,
		},
		{
			name:           "page 2",
			query:          "?page=2&page_size=2",
			wantStatusCode: http.StatusOK,
			wantCount:      2,
			wantPage:       2,
			wantPageSize:   2,
			wantTotalItems: 5,
			wantTotalPages: 3,
			wantHasNext:    true,
			wantHasPrev:    true,
		},
		{
			name:           "search with pagination",
			query:          "?q=worker-1&page=1&page_size=10",
			wantStatusCode: http.StatusOK,
			wantCount:      1,
			wantPage:       1,
			wantPageSize:   10,
			wantTotalItems: 1,
			wantTotalPages: 1,
		},
		{
			name:           "invalid page param",
			query:          "?page=-1",
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "invalid page_size param exceeding max",
			query:          "?page_size=101",
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "/api/v1/workers"+tt.query, nil)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatusCode, w.Code)
			if tt.wantStatusCode != http.StatusOK {
				return
			}

			var resp catalogtypes.WorkerListResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Len(t, resp.Data, tt.wantCount)
			assert.Equal(t, tt.wantPage, resp.Pagination.Page)
			assert.Equal(t, tt.wantPageSize, resp.Pagination.PageSize)
			assert.Equal(t, tt.wantTotalItems, resp.Pagination.TotalItems)
			assert.Equal(t, tt.wantTotalPages, resp.Pagination.TotalPages)
			assert.Equal(t, tt.wantHasNext, resp.Pagination.HasNext)
			assert.Equal(t, tt.wantHasPrev, resp.Pagination.HasPrev)
		})
	}
}
