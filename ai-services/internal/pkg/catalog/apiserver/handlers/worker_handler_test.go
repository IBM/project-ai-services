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
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
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

func (r *mockWorkerRepo) GetAll(_ context.Context) ([]models.Worker, error) {
	out := make([]models.Worker, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, *w)
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

func TestWorkerHandler_CreateWorker_Success(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	repo := newMockWorkerRepo()
	reg := registry.New(repo)
	handler := NewWorkerHandler(reg, repo, types.RuntimeTypePodman, 9090)
	router := setupWorkerRouter(handler)

	reqBody := `{"worker_name": "worker-1"}`
	req, err := http.NewRequest(http.MethodPost, "/api/v1/workers", bytes.NewBufferString(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp createWorkerResp
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "worker-1", resp.WorkerName)
	assert.NotEmpty(t, resp.Token)
}

func TestWorkerHandler_CreateWorker_AlreadyReadyConflict(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	repo := newMockWorkerRepo()
	reg := registry.New(repo)
	handler := NewWorkerHandler(reg, repo, types.RuntimeTypePodman, 9090)
	router := setupWorkerRouter(handler)

	// Pre-register and register to make worker ready
	_, err := reg.Preregister(context.Background(), "worker-ready")
	require.NoError(t, err)
	_, err = reg.Register(context.Background(), "worker-ready", "podman", nil)
	require.NoError(t, err)

	reqBody := `{"worker_name": "worker-ready"}`
	req, err := http.NewRequest(http.MethodPost, "/api/v1/workers", bytes.NewBufferString(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "already registered and ready")
}

func TestWorkerHandler_CreateWorker_NonReadyAllowed(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	repo := newMockWorkerRepo()
	reg := registry.New(repo)
	handler := NewWorkerHandler(reg, repo, types.RuntimeTypePodman, 9090)
	router := setupWorkerRouter(handler)

	// 1. Worker is in disconnected status
	_, err := reg.Preregister(context.Background(), "worker-disc")
	require.NoError(t, err)
	_, err = reg.Register(context.Background(), "worker-disc", "podman", nil)
	require.NoError(t, err)
	reg.Disconnect(context.Background(), "worker-disc")

	reqBody := `{"worker_name": "worker-disc"}`
	req, err := http.NewRequest(http.MethodPost, "/api/v1/workers", bytes.NewBufferString(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}
