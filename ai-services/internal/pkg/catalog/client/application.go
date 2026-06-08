package client

import (
	"fmt"
	"strconv"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ApplicationClient provides methods for interacting with the applications API.
type ApplicationClient struct {
	httpClient *resty.Client
	client     *Client
}

// NewApplicationClient creates a new ApplicationClient with the given server URL and token.
func NewApplicationClient() (*ApplicationClient, error) {
	client, err := New()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials: %v", err)
	}

	return &ApplicationClient{
		httpClient: resty.New().SetBaseURL(client.ServerURL()),
		client:     client,
	}, nil
}

// ListApplicationsParams holds optional query parameters for listing applications.
type ListApplicationsParams struct {
	// Page is the page number (1-indexed). Default: 1
	Page int
	// PageSize is the number of items per page (max: 100). Default: 20
	PageSize int
	// DeploymentType filters by deployment type: 'architectures' or 'services'
	DeploymentType string
	// CatalogID filters by catalog ID (e.g., 'rag', 'chat', 'digitize', 'summarize')
	CatalogID string
}

// ListApplications retrieves a paginated list of all applications for the authenticated user.
// It supports optional filters via the params argument.
//
// Example:
//
//	client := NewApplicationClient()
//	resp, err := client.ListApplications(&ListApplicationsParams{
//	    Page: 1,
//	    PageSize: 20,
//	    DeploymentType: "services",
//	    CatalogID: "rag",
//	})
func (c *ApplicationClient) ListApplications(params *ListApplicationsParams) (*types.ApplicationListResponse, error) {
	var result types.ApplicationListResponse
	req := c.httpClient.R().
		SetHeader("Authorization", "Bearer "+c.client.AccessToken()).
		SetResult(&result)

	if params != nil {
		if params.Page > 0 {
			req.SetQueryParam("page", strconv.Itoa(params.Page))
		}
		if params.PageSize > 0 {
			req.SetQueryParam("page_size", strconv.Itoa(params.PageSize))
		}
		if params.DeploymentType != "" {
			req.SetQueryParam("deployment_type", params.DeploymentType)
		}
		if params.CatalogID != "" {
			req.SetQueryParam("catalog_id", params.CatalogID)
		}
	}

	resp, err := req.Get("/api/v1/applications")
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("list applications: server returned HTTP %d", resp.StatusCode())
	}

	return &result, nil
}

// GetApplicationPS retrieves the process status and runtime information for an application.
// It returns details about pods, containers, and their health status.
func (c *ApplicationClient) GetApplicationPS(id string) (*types.ApplicationPSResponse, error) {
	var result types.ApplicationPSResponse
	resp, err := c.httpClient.R().
		SetHeader("Authorization", "Bearer "+c.client.AccessToken()).
		SetResult(&result).
		Get(fmt.Sprintf("/api/v1/applications/%s/ps", id))
	if err != nil {
		return nil, fmt.Errorf("get application ps: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get application ps: server returned HTTP %d", resp.StatusCode())
	}

	return &result, nil
}

// Made with Bob
