package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apirepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/gateway"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
)

const (
	// workerNameMinLen is the minimum allowed length for a worker name after trimming whitespace.
	workerNameMinLen = 3
	// workerNameMaxLen is the maximum allowed length for a worker name after trimming whitespace.
	workerNameMaxLen = 64
)

// WorkerHandler handles worker management endpoints.
type WorkerHandler struct {
	reg         *registry.Registry
	repo        repository.WorkerRepository
	runtimeType types.RuntimeType
	gatewayPort int
}

// NewWorkerHandler creates a new WorkerHandler.
func NewWorkerHandler(reg *registry.Registry, repo repository.WorkerRepository, runtimeType types.RuntimeType, gatewayPort int) *WorkerHandler {
	return &WorkerHandler{reg: reg, repo: repo, runtimeType: runtimeType, gatewayPort: gatewayPort}
}

// createWorkerReq is the request body for registering a new worker.
type createWorkerReq struct {
	WorkerName string `json:"worker_name" binding:"required"`
}

// createWorkerResp is the response body for a newly registered worker.
type createWorkerResp struct {
	WorkerName     string `json:"worker_name"`
	GatewayAddress string `json:"gateway_address"`
	Token          string `json:"token"`
}

// CreateWorker godoc
//
//	@Summary		Register a new worker
//	@Description	Pre-registers a worker by name, creates a pending DB row, and returns a single-use bootstrap token.
//	@Description	The operator passes this token when starting the worker daemon (`worker join --token <token>`).
//	@Tags			Workers
//	@Accept			json
//	@Produce		json
//	@Param			worker	body		createWorkerReq			true	"Worker registration request"
//	@Success		201		{object}	createWorkerResp		"Worker registered; token valid for 24 hours"
//	@Failure		400		{object}	map[string]interface{}	"Invalid payload"
//	@Failure		409		{object}	map[string]interface{}	"Worker is already registered and ready"
//	@Failure		500		{object}	map[string]interface{}	"Internal error"
//	@Security		BearerAuth
//	@Router			/workers [post]
func (h *WorkerHandler) CreateWorker(c *gin.Context) {
	var req createWorkerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})

		return
	}

	// Normalise: trim surrounding whitespace only. Case is preserved so that
	// the DB row and in-memory entry reflect the name as the operator gave it.
	// All lookups use case-insensitive comparison so "Worker-A" and "worker-a"
	// resolve to the same entry regardless of how it was registered.
	req.WorkerName = strings.TrimSpace(req.WorkerName)
	if len(req.WorkerName) < workerNameMinLen || len(req.WorkerName) > workerNameMaxLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker name must be between 3 and 64 characters"})

		return
	}

	// "Local" is reserved for the catalog-machine worker registered by the
	// configure flow. It is only allowed when LOCAL_WORKER=true, meaning this
	// catalog instance is configured to host a co-located worker.
	// Normalise to the canonical casing so the DB row and token store always
	// use "Local" regardless of how the operator typed it.
	if strings.EqualFold(req.WorkerName, workerconstants.LocalWorkerName) {
		if utils.GetEnv(workerconstants.LocalWorkerEnvVar, "") != "true" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("worker name %q is reserved", req.WorkerName)})

			return
		}

		req.WorkerName = workerconstants.LocalWorkerName
	}

	ctx := c.Request.Context()

	gatewayAddress, err := h.gatewayAddress(ctx)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to resolve gateway address: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve worker gateway address"})

		return
	}

	token, err := h.reg.Preregister(ctx, req.WorkerName)
	if err != nil {
		if errors.Is(err, registry.ErrWorkerAlreadyReady) {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("worker %q is already registered and ready", req.WorkerName)})

			return
		}
		logger.ErrorfCtx(ctx, "worker handler: failed to register worker %q: %v", req.WorkerName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register worker"})

		return
	}

	c.JSON(http.StatusCreated, createWorkerResp{
		WorkerName:     req.WorkerName,
		GatewayAddress: gatewayAddress,
		Token:          token,
	})
}

func (h *WorkerHandler) gatewayAddress(ctx context.Context) (string, error) {
	if h.runtimeType == types.RuntimeTypeOpenShift {
		host, err := gateway.GatewayRouteHost(ctx)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("%s:%d", host, workerconstants.OpenShiftRoutePort), nil
	}

	port := fmt.Sprintf("%d", h.gatewayPort)
	domainSuffix := utils.GetEnv("DOMAIN_SUFFIX", "")
	if domainSuffix == "" {
		return "", fmt.Errorf("DOMAIN_SUFFIX environment variable not set")
	}

	return workerconstants.WorkerGatewayName + "." + domainSuffix + ":" + port, nil
}

// ListWorkers godoc
//
//	@Summary		List all workers
//	@Description	Returns all registered workers, their current status, human-readable message, and connected application IDs.
//	@Tags			Workers
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int								false	"Page number (1-indexed)"				default(1)
//	@Param			page_size	query		int								false	"Number of items per page (max: 100)"	default(20)
//	@Param			q			query		string							false	"Case-insensitive substring search on worker name"
//	@Success		200			{object}	catalogtypes.WorkerListResponse	"List of workers with pagination"
//	@Failure		400			{object}	ErrorResponse					"Invalid query parameters"
//	@Failure		401			{object}	ErrorResponse					"Unauthorized"
//	@Failure		500			{object}	ErrorResponse					"Internal error"
//	@Router			/workers [get]
func (h *WorkerHandler) ListWorkers(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	page, pageSize, err := apirepo.ValidatePaginationParams(page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})

		return
	}

	ctx := c.Request.Context()

	req := &repository.ListWorkersRequest{
		Search: c.Query("q"),
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}

	totalCount, err := h.reg.GetCount(ctx, req)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to count workers: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to count workers"})

		return
	}

	workers, err := h.reg.List(ctx, req)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to list workers: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list workers"})

		return
	}

	appIDMap, err := h.fetchAppIDs(ctx, workers)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to fetch application IDs for workers: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to fetch application IDs"})

		return
	}

	c.JSON(http.StatusOK, buildWorkerListResponse(workers, appIDMap, page, pageSize, totalCount))
}

// buildWorkerListResponse converts workers into the paginated API response.
func buildWorkerListResponse(workers []models.Worker, appIDMap map[uuid.UUID][]uuid.UUID, page, pageSize, totalCount int) catalogtypes.WorkerListResponse {
	result := make([]catalogtypes.Worker, len(workers))
	for i, w := range workers {
		result[i] = toAPIWorker(w, appIDMap[w.ID])
	}

	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + pageSize - 1) / pageSize
	}

	return catalogtypes.WorkerListResponse{
		Data: result,
		Pagination: catalogtypes.PaginationMetadata{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: totalCount,
			TotalPages: totalPages,
			HasNext:    page < totalPages,
			HasPrev:    page > 1,
		},
	}
}

// GetWorker godoc
//
//	@Summary		Get a single worker
//	@Description	Returns the worker with the given ID, including its status message and connected application IDs.
//	@Tags			Workers
//	@Produce		json
//	@Param			id	path		string					true	"Worker ID (UUID)"
//	@Success		200	{object}	catalogtypes.Worker		"Worker details"
//	@Failure		400	{object}	map[string]interface{}	"Invalid worker ID"
//	@Failure		404	{object}	map[string]interface{}	"Worker not found"
//	@Failure		500	{object}	map[string]interface{}	"Internal error"
//	@Security		BearerAuth
//	@Router			/workers/{id} [get]
func (h *WorkerHandler) GetWorker(c *gin.Context) {
	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid worker id"})

		return
	}

	ctx := c.Request.Context()

	w, err := h.repo.GetByID(ctx, workerID)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to fetch worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch worker"})

		return
	}

	if w == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})

		return
	}

	appIDMap, err := h.repo.GetApplicationIDsByWorkerIDs(ctx, []uuid.UUID{workerID})
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to fetch application IDs for worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch application IDs"})

		return
	}

	c.JSON(http.StatusOK, toAPIWorker(*w, appIDMap[workerID]))
}

// DeleteWorker godoc
//
//	@Summary		Deregister a worker
//	@Description	Permanently removes a worker from the registry and the database.
//	@Description	If the worker is currently connected its gRPC stream is also cleaned up.
//	@Tags			Workers
//	@Produce		json
//	@Param			id	path	string	true	"Worker ID (UUID)"
//	@Success		204	"Worker deleted"
//	@Failure		400	{object}	map[string]interface{}	"Invalid worker ID"
//	@Failure		403	{object}	map[string]interface{}	"Local worker cannot be deleted"
//	@Failure		404	{object}	map[string]interface{}	"Worker not found"
//	@Failure		409	{object}	map[string]interface{}	"Worker still has applications deployed on it"
//	@Failure		500	{object}	map[string]interface{}	"Internal error"
//	@Security		BearerAuth
//	@Router			/workers/{id} [delete]
func (h *WorkerHandler) DeleteWorker(c *gin.Context) {
	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid worker id"})

		return
	}

	ctx := c.Request.Context()

	// Resolve the worker name before deletion so we can block the Local worker.
	w, err := h.repo.GetByID(ctx, workerID)
	if err != nil {
		logger.ErrorfCtx(ctx, "worker handler: failed to fetch worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete worker"})

		return
	}

	if w == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})

		return
	}

	if strings.EqualFold(w.Name, workerconstants.LocalWorkerName) {
		c.JSON(http.StatusForbidden, gin.H{"error": "the Local worker cannot be deleted"})

		return
	}

	deleted, err := h.reg.Deregister(ctx, workerID)
	if err != nil {
		if errors.Is(err, registry.ErrWorkerHasApplications) {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("worker %q cannot be deleted while it has applications deployed on it", w.Name),
			})

			return
		}

		logger.ErrorfCtx(ctx, "worker handler: failed to deregister worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete worker"})

		return
	}

	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})

		return
	}

	c.Status(http.StatusNoContent)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// fetchAppIDs does a single bulk query for all workers and returns a map of workerID → appIDs.
func (h *WorkerHandler) fetchAppIDs(ctx context.Context, workers []models.Worker) (map[uuid.UUID][]uuid.UUID, error) {
	if h.repo == nil || len(workers) == 0 {
		return map[uuid.UUID][]uuid.UUID{}, nil
	}

	ids := make([]uuid.UUID, len(workers))
	for i, w := range workers {
		ids[i] = w.ID
	}

	return h.repo.GetApplicationIDsByWorkerIDs(ctx, ids)
}

// toAPIWorker converts a DB worker model to the public API type, populating
// Message and ApplicationIDs from the provided slice.
func toAPIWorker(w models.Worker, appIDs []uuid.UUID) catalogtypes.Worker {
	out := catalogtypes.Worker{
		ID:            w.ID.String(),
		Name:          w.Name,
		RuntimeType:   catalogtypes.WorkerRuntimeType(w.RuntimeType),
		Status:        catalogtypes.WorkerStatus(w.Status),
		Message:       w.Message,
		LastHeartbeat: w.LastHeartbeat,
		Metadata:      w.Metadata,
		RegisteredAt:  w.RegisteredAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     w.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if len(appIDs) > 0 {
		out.ApplicationIDs = make([]string, len(appIDs))
		for i, id := range appIDs {
			out.ApplicationIDs[i] = id.String()
		}
	}

	return out
}
