package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// API route constants for model endpoints.
const (
	modelsRoute        = "/api/v1/models"
	getModelRoute      = "/api/v1/models/%s"
	catalogModelsRoute = "/api/v1/catalog/models"

	// maxModelsPageSize is the largest page the models API serves.
	maxModelsPageSize = 100
)

// ListModelsParams holds the optional filters for listing the caller's models. Empty fields are
// not sent. Slice fields match any of their values.
type ListModelsParams struct {
	// ComponentType filters by model role (llm, embedding, reranker).
	ComponentType string
	// DeploymentType filters by kind: "local" or "remote".
	DeploymentType string
	// ProviderIDs filters by provider (e.g. vllm-spyre, watsonx).
	ProviderIDs []string
	// Models filters by catalog model identifier.
	Models []string
	// Statuses filters by status: Running, Error, ... (local) or connected, offline (remote).
	Statuses []string
	// Search is a case-insensitive substring match on the model name.
	Search string
}

// ModelClient provides methods for interacting with the models API.
type ModelClient struct {
	client *Client
}

// NewModelClient creates a ModelClient using stored credentials.
func NewModelClient(ctx context.Context) (*ModelClient, error) {
	c, err := New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &ModelClient{client: c}, nil
}

// ListModels returns every model of the caller that matches params, following the pagination of
// the API. A nil params lists all of them.
func (c *ModelClient) ListModels(ctx context.Context, params *ListModelsParams) ([]Model, error) {
	var all []Model

	for page := 1; ; page++ {
		var result modelListResponse

		resp, err := c.client.HTTPClient().R().
			SetContext(ctx).
			SetResult(&result).
			SetQueryParamsFromValues(params.query(page)).
			Get(modelsRoute)
		if err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}

		if resp.IsError() {
			return nil, &HTTPError{StatusCode: resp.StatusCode(), Message: utils.ParseErrorResponse(resp)}
		}

		all = append(all, result.Data...)

		if !result.Pagination.HasNext {
			return all, nil
		}
	}
}

// GetModel returns the details of the caller's model with the given ID.
func (c *ModelClient) GetModel(ctx context.Context, id string) (*Model, error) {
	var result Model

	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(getModelRoute, url.PathEscape(id)))
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{StatusCode: resp.StatusCode(), Message: utils.ParseErrorResponse(resp)}
	}

	return &result, nil
}

// ListCatalogModels returns the deployable models of the catalog, optionally only those of one
// role (llm, embedding, reranker).
func (c *ModelClient) ListCatalogModels(ctx context.Context, componentType string) ([]types.ModelCatalogEntry, error) {
	var result []types.ModelCatalogEntry

	req := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result)

	if componentType != "" {
		req.SetQueryParam("component_type", componentType)
	}

	resp, err := req.Get(catalogModelsRoute)
	if err != nil {
		return nil, fmt.Errorf("list catalog models: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{StatusCode: resp.StatusCode(), Message: utils.ParseErrorResponse(resp)}
	}

	return result, nil
}

// query builds the query string of one page of the list request. It is safe on a nil receiver.
func (p *ListModelsParams) query(page int) url.Values {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(maxModelsPageSize))

	if p == nil {
		return q
	}

	if p.ComponentType != "" {
		q.Set("component_type", p.ComponentType)
	}

	if p.DeploymentType != "" {
		q.Set("deployment_type", p.DeploymentType)
	}

	if p.Search != "" {
		q.Set("q", p.Search)
	}

	for _, v := range p.ProviderIDs {
		q.Add("provider_id", v)
	}

	for _, v := range p.Models {
		q.Add("model", v)
	}

	for _, v := range p.Statuses {
		q.Add("status", v)
	}

	return q
}
