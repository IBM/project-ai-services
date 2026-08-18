package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
)

// Ensure dbmodels is imported for Swagger documentation.
var _ dbmodels.Worker

// WorkerHandler handles worker management endpoints.
type WorkerHandler struct {
	reg *registry.Registry
}

// NewWorkerHandler creates a new WorkerHandler.
func NewWorkerHandler(reg *registry.Registry) *WorkerHandler {
	return &WorkerHandler{reg: reg}
}

// createWorkerReq is the request body for registering a new worker.
type createWorkerReq struct {
	WorkerName string `json:"worker_name" binding:"required,min=1,max=100"`
}

// createWorkerResp is the response body for a newly registered worker.
type createWorkerResp struct {
	WorkerName string `json:"worker_name"`
	Token      string `json:"token"`
}

// CreateWorker godoc
//
//	@Summary		Register a new worker
//	@Description	Pre-registers a worker by name, creates a pending DB row, and returns a single-use bootstrap token.
//	@Description	The operator passes this token when starting the worker daemon (`worker start --token <token>`).
//	@Tags			Workers
//	@Accept			json
//	@Produce		json
//	@Param			worker	body		createWorkerReq			true	"Worker registration request"
//	@Success		201		{object}	createWorkerResp		"Worker registered; token valid for 24 hours"
//	@Failure		400		{object}	map[string]interface{}	"Invalid payload"
//	@Failure		500		{object}	map[string]interface{}	"Internal error"
//	@Security		BearerAuth
//	@Router			/workers [post]
func (h *WorkerHandler) CreateWorker(c *gin.Context) {
	var req createWorkerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})

		return
	}

	// Normalise: trim surrounding whitespace and lowercase so that
	// "Worker-A", "worker-a", and " worker-a " all resolve to the same name.
	req.WorkerName = strings.ToLower(strings.TrimSpace(req.WorkerName))
	if req.WorkerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_name must not be blank"})

		return
	}

	ctx := c.Request.Context()

	token, err := h.reg.Preregister(ctx, req.WorkerName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register worker"})

		return
	}

	c.JSON(http.StatusCreated, createWorkerResp{
		WorkerName: req.WorkerName,
		Token:      token,
	})
}

// ListWorkers godoc
//
//	@Summary		List all workers
//	@Description	Returns all registered workers and their current status from the database.
//	@Tags			Workers
//	@Produce		json
//	@Success		200	{array}		dbmodels.Worker			"List of workers"
//	@Failure		500	{object}	map[string]interface{}	"Internal error"
//	@Security		BearerAuth
//	@Router			/workers [get]
func (h *WorkerHandler) ListWorkers(c *gin.Context) {
	workers, err := h.reg.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list workers"})

		return
	}

	c.JSON(http.StatusOK, workers)
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
//	@Failure		404	{object}	map[string]interface{}	"Worker not found"
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

	deleted, err := h.reg.Deregister(ctx, workerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete worker"})

		return
	}

	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})

		return
	}

	c.Status(http.StatusNoContent)
}
