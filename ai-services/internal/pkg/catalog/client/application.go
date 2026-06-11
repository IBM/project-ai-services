package client

import (
	"fmt"
	"strconv"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// API route constants for application endpoints.
const (
	listApplicationsRoute = "/api/v1/applications"
	getApplicationPSRoute = "/api/v1/applications/%s/ps"
	getApplicationRoute   = "/api/v1/applications/%s"
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

	resp, err := req.Get(listApplicationsRoute)
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
		Get(fmt.Sprintf(getApplicationPSRoute, id))
	if err != nil {
		return nil, fmt.Errorf("get application ps: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get application ps: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// DeleteApplication deletes an application by its ID.
// It removes the application and all its associated resources.
// Supports optional parameters via the params argument.
//
// Example:
//
//	client := NewApplicationClient()
//	err := client.DeleteApplication("rag", &DeleteApplicationParams{
//	    KeepData: true,
//	})
func (c *ApplicationClient) DeleteApplication(id string, params *DeleteApplicationParams) error {
	req := c.httpClient.R().
		SetHeader("Authorization", "Bearer "+c.client.AccessToken())

	if params != nil {
		if params.KeepData {
			req.SetQueryParam("keep_data", "true")
		}
	}

	resp, err := req.Delete(fmt.Sprintf(getApplicationRoute, id))
	if err != nil {
		return fmt.Errorf("delete application: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("delete application: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return nil
}

// GetApplication retrieves full details for a specific application by ID.
func (c *ApplicationClient) GetApplication(id string) (*types.Application, error) {
	var result types.Application
	resp, err := c.httpClient.R().
		SetHeader("Authorization", "Bearer "+c.client.AccessToken()).
		SetResult(&result).
		Get(fmt.Sprintf(getApplicationRoute, id))
	if err != nil {
		return nil, fmt.Errorf("get application: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get application: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// Made with Bob
