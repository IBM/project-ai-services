package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// API route constants for bundle endpoints.
const (
	bundlesRoute        = "/api/v1/catalog/bundles"
	bundleValidateRoute = "/api/v1/catalog/bundles/validate"
	bundleByIDRoute     = "/api/v1/catalog/bundles/%s"
)

// BundleResponse is the client-layer representation of a catalog bundle record.
type BundleResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	SizeBytes   *int64    `json:"size_bytes,omitempty"`
	CatalogType string    `json:"catalog_type"`
	CatalogID   string    `json:"catalog_id"`
	Version     string    `json:"version"`
	CreatedBy   string    `json:"created_by,omitempty"`
}

// BundleListResponse is the paginated response returned by GET /api/v1/catalog/bundles.
type BundleListResponse struct {
	Bundles    []BundleResponse         `json:"bundles"`
	Pagination BundlePaginationMetadata `json:"pagination"`
}

// BundlePaginationMetadata holds page-level metadata for bundle list responses.
type BundlePaginationMetadata struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalItems int  `json:"total_items"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// BundleValidateResult is the response from POST /api/v1/catalog/bundles/validate.
// component_type is populated only for component bundles; it is empty for service bundles.
type BundleValidateResult struct {
	Valid         bool   `json:"valid"`
	CatalogType   string `json:"catalog_type"`
	ComponentType string `json:"component_type,omitempty"`
	CatalogID     string `json:"catalog_id"`
	Version       string `json:"version"`
	Name          string `json:"name,omitempty"`
}

// BundleClient provides methods for interacting with the catalog bundle API.
type BundleClient struct {
	client *Client
}

// NewBundleClient creates a new BundleClient using stored credentials.
func NewBundleClient(ctx context.Context) (*BundleClient, error) {
	c, err := New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &BundleClient{client: c}, nil
}

// CreateBundle POSTs a .tar.gz archive as multipart/form-data to create a new bundle.
// Returns the 201 BundleResponse (status always "active" on success).
func (c *BundleClient) CreateBundle(ctx context.Context, filePath string) (*BundleResponse, error) {
	var result BundleResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetFile("file", filePath).
		SetResult(&result).
		Post(bundlesRoute)
	if err != nil {
		return nil, fmt.Errorf("create bundle: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// UpdateBundle PUTs a replacement .tar.gz archive for the bundle identified by bundleID.
// Returns the 200 BundleResponse with status "active" (fully synchronous).
func (c *BundleClient) UpdateBundle(ctx context.Context, bundleID, filePath string) (*BundleResponse, error) {
	var result BundleResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetFile("file", filePath).
		SetResult(&result).
		Put(fmt.Sprintf(bundleByIDRoute, bundleID))
	if err != nil {
		return nil, fmt.Errorf("update bundle: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// DeleteBundle sends DELETE /api/v1/catalog/bundles/:bundleID.
func (c *BundleClient) DeleteBundle(ctx context.Context, bundleID string) error {
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		Delete(fmt.Sprintf(bundleByIDRoute, bundleID))
	if err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}

	if resp.IsError() {
		return &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return nil
}

// GetBundle returns the full BundleResponse for a single bundle by its ID.
func (c *BundleClient) GetBundle(ctx context.Context, bundleID string) (*BundleResponse, error) {
	var result BundleResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(bundleByIDRoute, bundleID))
	if err != nil {
		return nil, fmt.Errorf("get bundle: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// ListBundles returns a paginated list of all registered bundles.
func (c *BundleClient) ListBundles(ctx context.Context, page, pageSize int) (*BundleListResponse, error) {
	var result BundleListResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetQueryParam("page", strconv.Itoa(page)).
		SetQueryParam("page_size", strconv.Itoa(pageSize)).
		SetResult(&result).
		Get(bundlesRoute)
	if err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// ValidateBundle POSTs to the validate endpoint without creating a bundle (no DB write, no reload).
// Returns a BundleValidateResult on success. The component_type field is only populated for
// component bundles; it is empty for service bundles.
func (c *BundleClient) ValidateBundle(ctx context.Context, filePath string) (*BundleValidateResult, error) {
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetFile("file", filePath).
		Post(bundleValidateRoute)
	if err != nil {
		return nil, fmt.Errorf("validate bundle: %w", err)
	}

	if resp.StatusCode() == http.StatusUnprocessableEntity {
		return nil, &HTTPError{
			StatusCode: http.StatusUnprocessableEntity,
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	var result BundleValidateResult
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("validate bundle: decode response: %w", err)
	}

	return &result, nil
}
