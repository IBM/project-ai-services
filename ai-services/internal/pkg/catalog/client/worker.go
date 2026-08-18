package client

import (
	"fmt"

	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

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

// CreateWorker pre-registers a new worker by name and returns its bootstrap token.
func (c *Client) CreateWorker(name string) (*CreateWorkerResponse, error) {
	var result CreateWorkerResponse
	resp, err := c.httpClient.R().
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
func (c *Client) ListWorkers() ([]catalogtypes.Worker, error) {
	var result []catalogtypes.Worker
	resp, err := c.httpClient.R().
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
func (c *Client) DeleteWorkerByName(name string) error {
	workers, err := c.ListWorkers()
	if err != nil {
		return err
	}

	for _, w := range workers {
		if string(w.Name) == name {
			return c.deleteWorkerByID(w.ID)
		}
	}

	return fmt.Errorf("worker %q not found", name)
}

// deleteWorkerByID permanently removes a worker by its UUID string.
func (c *Client) deleteWorkerByID(id string) error {
	resp, err := c.httpClient.R().
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
