package client

import (
	"fmt"
	"strconv"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
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
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &ApplicationClient{
		httpClient: resty.New().SetBaseURL(client.ServerURL()),
		client:     client,
	}, nil
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
		return nil, fmt.Errorf("list applications: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
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
		return nil, fmt.Errorf("get application ps: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// Made with Bob
