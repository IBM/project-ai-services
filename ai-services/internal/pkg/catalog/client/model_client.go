package client

import (
	"context"
	"fmt"
	"strconv"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	modelsRoute   = "/api/v1/models"
	getModelRoute = "/api/v1/models/%s"
)

// ModelClient provides methods for interacting with the models API.
type ModelClient struct {
	client *Client
}

// NewModelClient creates a new ModelClient using the stored catalog credentials.
func NewModelClient(ctx context.Context) (*ModelClient, error) {
	client, err := New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &ModelClient{client: client}, nil
}

// ListModels retrieves a page of the caller's models (local and remote).
func (c *ModelClient) ListModels(ctx context.Context, params *ListModelsParams) (*models.ModelListResponse, error) {
	var result models.ModelListResponse
	req := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result)

	if params != nil {
		if params.Page > 0 {
			req.SetQueryParam("page", strconv.Itoa(params.Page))
		}
		if params.PageSize > 0 {
			req.SetQueryParam("page_size", strconv.Itoa(params.PageSize))
		}
		if params.Search != "" {
			req.SetQueryParam("q", params.Search)
		}
	}

	resp, err := req.Get(modelsRoute)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// GetModel retrieves the full details of a model, local or remote, by ID.
func (c *ModelClient) GetModel(ctx context.Context, id string) (*models.ModelDetail, error) {
	var result models.ModelDetail
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(getModelRoute, id))
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}
