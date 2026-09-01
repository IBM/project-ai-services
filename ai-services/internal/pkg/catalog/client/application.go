package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// API route constants for application endpoints.
const (
	applicationsRoute       = "/api/v1/applications"
	getApplicationPSRoute   = "/api/v1/applications/%s/ps"
	getApplicationRoute     = "/api/v1/applications/%s"
	svcDeployOptionsRoute   = "/api/v1/services/%s/deploy-options"
	archDeployOptionsRoute  = "/api/v1/architectures/%s/deploy-options"
	compProviderParamsRoute = "/api/v1/components/%s/providers/%s/params"
	svcImagesRoute          = "/api/v1/services/%s/images"
	archImagesRoute         = "/api/v1/architectures/%s/images"
	svcModelsRoute          = "/api/v1/services/%s/models"
	archModelsRoute         = "/api/v1/architectures/%s/models"
)

// ImagesResponse is the JSON body returned by GET /api/v1/services/:id/images
// and GET /api/v1/architectures/:id/images.
type ImagesResponse struct {
	Images []string `json:"images"`
}

// HTTPError represents an HTTP error with status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// ApplicationClient provides methods for interacting with the applications API.
type ApplicationClient struct {
	client *Client
}

// NewApplicationClient creates a new ApplicationClient with the given server URL and token.
func NewApplicationClient(ctx context.Context) (*ApplicationClient, error) {
	client, err := New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return &ApplicationClient{
		client: client,
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
func (c *ApplicationClient) ListApplications(ctx context.Context, params *ListApplicationsParams) (*types.ApplicationListResponse, error) {
	var result types.ApplicationListResponse
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
		if params.DeploymentType != "" {
			req.SetQueryParam("deployment_type", params.DeploymentType)
		}
		if params.CatalogID != "" {
			req.SetQueryParam("catalog_id", params.CatalogID)
		}
	}

	resp, err := req.Get(applicationsRoute)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// GetApplicationPS retrieves the process status and runtime information for an application.
// It returns details about pods, containers, and their health status.
func (c *ApplicationClient) GetApplicationPS(ctx context.Context, id string) (*types.ApplicationPSResponse, error) {
	var result types.ApplicationPSResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(getApplicationPSRoute, id))
	if err != nil {
		return nil, fmt.Errorf("get application ps: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
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
func (c *ApplicationClient) DeleteApplication(ctx context.Context, id string, params *DeleteApplicationParams) error {
	req := c.client.HTTPClient().R().SetContext(ctx)

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
		return &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return nil
}

// GetApplication retrieves full details for a specific application by ID.
func (c *ApplicationClient) GetApplication(ctx context.Context, id string) (*types.Application, error) {
	var result types.Application
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(getApplicationRoute, id))
	if err != nil {
		return nil, fmt.Errorf("get application: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// GetApplicationWithRefresh retrieves full details for a specific application by ID.
// If the server returns 401 Unauthorized, it refreshes the access token once and retries.
func (c *ApplicationClient) GetApplicationWithRefresh(ctx context.Context, id string) (*types.Application, error) {
	result, err := c.GetApplication(ctx, id)
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized {
			if refreshErr := c.client.RefreshToken(ctx); refreshErr != nil {
				return nil, err
			}

			return c.GetApplication(ctx, id)
		}

		return nil, err
	}

	return result, nil
}

// CreateApplication creates a new application deployment via catalog API.
// It accepts a CreateApplicationRequest with catalog ID, name, services, and components configuration.
func (c *ApplicationClient) CreateApplication(ctx context.Context, req *models.CreateApplicationRequest) (*models.CreateApplicationResponse, error) {
	var result models.CreateApplicationResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&result).
		Post(applicationsRoute)

	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("create application: server returned HTTP %d: %s",
			resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// GetServiceDeployOptions retrieves deploy options for a specific service.
// It returns available providers and dependency rules for the service and its components.
func (c *ApplicationClient) GetServiceDeployOptions(ctx context.Context, serviceID string) (*types.DeployOptionsService, error) {
	var result types.DeployOptionsService
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(svcDeployOptionsRoute, serviceID))
	if err != nil {
		return nil, fmt.Errorf("get service deploy options: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get service deploy options: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// GetArchitectureDeployOptions retrieves deploy options for an architecture.
// It returns available providers and dependency rules for all services in the architecture.
func (c *ApplicationClient) GetArchitectureDeployOptions(ctx context.Context, architectureID string) (*types.DeployOptionsArchitecture, error) {
	var result types.DeployOptionsArchitecture
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(archDeployOptionsRoute, architectureID))
	if err != nil {
		return nil, fmt.Errorf("get architecture deploy options: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get architecture deploy options: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return &result, nil
}

// GetComponentProviderParams retrieves the parameter schema for a specific component provider.
func (c *ApplicationClient) GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error) {
	var result map[string]any
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(compProviderParamsRoute, componentType, providerID))
	if err != nil {
		return nil, fmt.Errorf("get component provider params: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get component provider params: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return result, nil
}

// GetServiceImages returns the complete list of container images required to deploy
// the given service and all its component dependencies. The response always includes
// catalog asset images (tool image and catalog infrastructure images).
// Both embedded (built-in) and custom bundle services are supported.
func (c *ApplicationClient) GetServiceImages(ctx context.Context, serviceID string) (*ImagesResponse, error) {
	var result ImagesResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(svcImagesRoute, serviceID))
	if err != nil {
		return nil, fmt.Errorf("get service images: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// GetArchitectureImages returns the complete list of container images required to deploy
// the given architecture and all its services and component dependencies. The response
// always includes catalog asset images (tool image and catalog infrastructure images).
// Both embedded (built-in) and custom bundle architectures are supported.
func (c *ApplicationClient) GetArchitectureImages(ctx context.Context, architectureID string) (*ImagesResponse, error) {
	var result ImagesResponse
	resp, err := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result).
		Get(fmt.Sprintf(archImagesRoute, architectureID))
	if err != nil {
		return nil, fmt.Errorf("get architecture images: %w", err)
	}

	if resp.IsError() {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode(),
			Message:    utils.ParseErrorResponse(resp),
		}
	}

	return &result, nil
}

// modelsResponse is the shape of the JSON body returned by the /models endpoints.
type modelsResponse struct {
	Models []string `json:"models"`
}

// GetServiceModels calls GET /api/v1/services/:id/models and returns the list of
// unique model names referenced by the service's component dependencies.
// Pass excludeProviders to have the server omit models from those component providers
// (e.g. "watsonx"). Maps to the ?exclude_providers= query parameter.
func (c *ApplicationClient) GetServiceModels(ctx context.Context, serviceID string, excludeProviders ...string) ([]string, error) {
	var result modelsResponse
	req := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result)

	if len(excludeProviders) > 0 {
		req.SetQueryParam("exclude_providers", strings.Join(excludeProviders, ","))
	}

	resp, err := req.Get(fmt.Sprintf(svcModelsRoute, serviceID))
	if err != nil {
		return nil, fmt.Errorf("get service models: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get service models: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return result.Models, nil
}

// GetArchitectureModels calls GET /api/v1/architectures/:id/models and returns the
// list of unique model names referenced across all services in the architecture.
// Pass excludeProviders to have the server omit models from those component providers
// (e.g. "watsonx"). Maps to the ?exclude_providers= query parameter.
func (c *ApplicationClient) GetArchitectureModels(ctx context.Context, architectureID string, excludeProviders ...string) ([]string, error) {
	var result modelsResponse
	req := c.client.HTTPClient().R().
		SetContext(ctx).
		SetResult(&result)

	if len(excludeProviders) > 0 {
		req.SetQueryParam("exclude_providers", strings.Join(excludeProviders, ","))
	}

	resp, err := req.Get(fmt.Sprintf(archModelsRoute, architectureID))
	if err != nil {
		return nil, fmt.Errorf("get architecture models: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get architecture models: server returned HTTP %d: %s", resp.StatusCode(), utils.ParseErrorResponse(resp))
	}

	return result.Models, nil
}

// Made with Bob
