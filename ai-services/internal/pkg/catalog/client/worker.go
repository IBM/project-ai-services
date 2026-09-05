package client

import (
	"context"
	"fmt"

	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// API route constants for worker endpoints.
const (
	workersRoute    = "/api/v1/workers"
	workerByIDRoute = "/api/v1/workers/%s"
)

// CreateWorkerRequest is the payload sent to POST /api/v1/workers.
type CreateWorkerRequest struct {
	WorkerName string `json:"worker_name"`
}

// CreateWorkerResponse is the payload returned by POST /api/v1/workers.
type CreateWorkerResponse struct {
	WorkerName string `json:"worker_name"`
	Token      string `json:"token"`
}

// WorkerClient provides methods for interacting with the worker management API.
type WorkerClient struct {
	client *Client
}

// NewWorkerClient creates a new WorkerClient using stored credentials.
func NewWorkerClient(ctx context.Context) (*WorkerClient, error) {
	c, err := New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &WorkerClient{client: c}, nil
}

// CreateWorker pre-registers a new worker by name and returns its bootstrap token.
func (c *WorkerClient) CreateWorker(ctx context.Context, name string) (*CreateWorkerResponse, error) {
	var result CreateWorkerResponse
	resp, err := c.client.httpClient.R().
		SetContext(ctx).
		SetBody(CreateWorkerRequest{WorkerName: name}).
		SetResult(&result).
		Post(workersRoute)
	if err != nil {
		return nil, fmt.Errorf("create worker: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("create worker: server returned HTTP %d: %s",
			resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// ListWorkers returns all registered workers from the catalog.
func (c *WorkerClient) ListWorkers(ctx context.Context) ([]catalogtypes.Worker, error) {
	var result []catalogtypes.Worker
	resp, err := c.client.httpClient.R().
		SetContext(ctx).
		SetResult(&result).
		Get(workersRoute)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("list workers: server returned HTTP %d: %s",
			resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return result, nil
}

// DeleteWorkerByName resolves a worker name to its UUID and permanently removes it.
// Returns an error if no worker with that name is registered.
func (c *WorkerClient) DeleteWorkerByName(ctx context.Context, name string) error {
	workers, err := c.ListWorkers(ctx)
	if err != nil {
		return err
	}

	for _, w := range workers {
		if w.Name == name {
			return c.deleteWorkerByID(ctx, w.ID)
		}
	}

	return fmt.Errorf("worker %q not found", name)
}

// deleteWorkerByID permanently removes a worker by its UUID string.
func (c *WorkerClient) deleteWorkerByID(ctx context.Context, id string) error {
	resp, err := c.client.httpClient.R().
		SetContext(ctx).
		Delete(fmt.Sprintf(workerByIDRoute, id))
	if err != nil {
		return fmt.Errorf("delete worker: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("delete worker: server returned HTTP %d: %s",
			resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return nil
}
