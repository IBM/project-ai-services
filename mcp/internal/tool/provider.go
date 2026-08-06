package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/project-ai-services/mcp/internal/authenticator"
	"github.com/project-ai-services/mcp/internal/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Provider provides a single tool based on an OpenAPI operation
type Provider struct {
	operation     types.OperationInfo
	endpoint      string
	regionServers []types.RegionServer
	authenticator authenticator.Authenticator
	globalQuery   map[string]string
	globalHeaders map[string]string
	bodyName      string
	inputSchema   *jsonschema.Schema
}

// NewProvider creates a new tool provider
func NewProvider(operation types.OperationInfo, endpoint string,
	regionServers []types.RegionServer, auth authenticator.Authenticator,
	globalQuery, globalHeaders map[string]string) (*Provider, error) {

	provider := &Provider{
		operation:     operation,
		endpoint:      endpoint,
		regionServers: regionServers,
		authenticator: auth,
		globalQuery:   globalQuery,
		globalHeaders: globalHeaders,
		bodyName:      getBodyName(operation),
	}

	provider.inputSchema = provider.buildInputSchema()

	return provider, nil
}

// getBodyName determines the appropriate name for the request body parameter
func getBodyName(operation types.OperationInfo) string {
	if strings.HasPrefix(operation.OperationID, "create_") || strings.HasPrefix(operation.OperationID, "replace_") {
		return "prototype"
	} else if operation.Method == types.PATCH {
		return "patch"
	}
	return "data"
}

// schemaBuilder helps build JSON schemas incrementally
type schemaBuilder struct {
	properties map[string]*jsonschema.Schema
	required   []string
}

// toSchema converts the builder to a final schema
func (sb *schemaBuilder) toSchema() *jsonschema.Schema {
	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: sb.properties,
	}
	if len(sb.required) > 0 {
		schema.Required = sb.required
	}
	return schema
}

// addProperty adds a property to the schema
func (sb *schemaBuilder) addProperty(name string, prop *jsonschema.Schema, required bool) {
	if prop != nil {
		sb.properties[name] = prop
		if required {
			sb.required = append(sb.required, name)
		}
	}
}

// GetTool returns the MCP tool definition
func (p *Provider) GetTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        p.operation.OperationID,
		Description: p.operation.Description,
		InputSchema: p.inputSchema,
	}
}

// buildInputSchema builds the JSON schema for the tool's input
func (p *Provider) buildInputSchema() *jsonschema.Schema {
	sb := &schemaBuilder{
		properties: make(map[string]*jsonschema.Schema),
		required:   []string{},
	}

	p.addServerRegionToSchema(sb)
	p.addPathParametersToSchema(sb)
	p.addQueryParametersToSchema(sb)
	p.addHeaderParametersToSchema(sb)
	p.addRequestBodyToSchema(sb)

	return sb.toSchema()
}

// addServerRegionToSchema adds server region parameter if needed
func (p *Provider) addServerRegionToSchema(sb *schemaBuilder) {
	if p.endpoint == "" && len(p.regionServers) > 0 {
		regions := extractRegions(p.regionServers)
		regionSchema := buildServerRegionSchema(regions)
		sb.addProperty("serverRegion", regionSchema, true)
	}
}

// addPathParametersToSchema adds path parameters to the schema
func (p *Provider) addPathParametersToSchema(sb *schemaBuilder) {
	for _, param := range p.operation.Parameters {
		if param.In == "path" {
			paramSchema := p.buildParameterSchema(param)
			sb.addProperty(param.Name, paramSchema, param.Required)
		}
	}
}

// addQueryParametersToSchema adds query parameters to the schema
func (p *Provider) addQueryParametersToSchema(sb *schemaBuilder) {
	queryParams := p.getQueryParameters()
	if len(queryParams) > 0 {
		querySchema := p.buildQuerySchema(queryParams)
		if querySchema != nil {
			required := hasRequiredFields(querySchema)
			sb.addProperty("query", querySchema, required)
		}
	}
}

// addHeaderParametersToSchema adds header parameters to the schema
func (p *Provider) addHeaderParametersToSchema(sb *schemaBuilder) {
	headerParams := p.getHeaderParameters()
	if len(headerParams) > 0 {
		headerSchema := p.buildHeaderSchema(headerParams)
		if headerSchema != nil {
			required := hasRequiredFields(headerSchema)
			sb.addProperty("headers", headerSchema, required)
		}
	}
}

// addRequestBodyToSchema adds request body to the schema
func (p *Provider) addRequestBodyToSchema(sb *schemaBuilder) {
	if p.operation.RequestBody != nil {
		bodySchema := p.buildRequestBodySchema()
		sb.addProperty(p.bodyName, bodySchema, p.operation.RequestBody.Required)
	}
}

// extractRegions extracts region names from region servers
func extractRegions(regionServers []types.RegionServer) []string {
	regions := make([]string, len(regionServers))
	for i, rs := range regionServers {
		regions[i] = rs.Region
	}
	return regions
}

// buildServerRegionSchema creates a schema for server region selection
func buildServerRegionSchema(regions []string) *jsonschema.Schema {
	// jsonschema requires the Enum property to be of type []any
	enumVals := make([]any, len(regions))
	for i, r := range regions {
		enumVals[i] = r
	}

	return &jsonschema.Schema{
		Type:        "string",
		Description: fmt.Sprintf("The region for the API endpoint. Available regions: %s", strings.Join(regions, ", ")),
		Enum:        enumVals,
	}
}

// getQueryParameters returns non-global query parameters
func (p *Provider) getQueryParameters() []types.ParameterInfo {
	var params []types.ParameterInfo
	for _, param := range p.operation.Parameters {
		if param.In == "query" {
			// Check if it's a global parameter
			if _, isGlobal := p.globalQuery[param.Name]; !isGlobal {
				params = append(params, param)
			}
		}
	}
	return params
}

// getHeaderParameters returns non-global, non-auth header parameters
func (p *Provider) getHeaderParameters() []types.ParameterInfo {
	var params []types.ParameterInfo
	for _, param := range p.operation.Parameters {
		if param.In == "header" && strings.ToLower(param.Name) != "authorization" {
			// Check if it's a global parameter
			if _, isGlobal := p.globalHeaders[strings.ToLower(param.Name)]; !isGlobal {
				params = append(params, param)
			}
		}
	}
	return params
}

// buildParameterSchema builds a schema for a parameter
func (p *Provider) buildParameterSchema(param types.ParameterInfo) *jsonschema.Schema {
	if param.Schema == nil {
		return &jsonschema.Schema{
			Description: param.Description,
		}
	}

	// Clone the schema and add description if needed
	schema := param.Schema
	if param.Description != "" && schema.Description == "" {
		// Create a shallow copy to avoid modifying the original
		copy := *schema
		copy.Description = param.Description
		schema = &copy
	}

	return schema
}

// buildQuerySchema builds a schema for query parameters
func (p *Provider) buildQuerySchema(params []types.ParameterInfo) *jsonschema.Schema {
	if len(params) == 0 {
		return nil
	}

	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: make(map[string]*jsonschema.Schema),
	}

	var required []string

	for _, param := range params {
		schema.Properties[param.Name] = p.buildParameterSchema(param)
		if param.Required {
			required = append(required, param.Name)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

// buildHeaderSchema builds a schema for header parameters
func (p *Provider) buildHeaderSchema(params []types.ParameterInfo) *jsonschema.Schema {
	if len(params) == 0 {
		return nil
	}

	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: make(map[string]*jsonschema.Schema),
	}

	var required []string

	for _, param := range params {
		// Use lowercase header names
		headerName := strings.ToLower(param.Name)
		schema.Properties[headerName] = p.buildParameterSchema(param)
		if param.Required {
			required = append(required, headerName)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

// buildRequestBodySchema builds a schema for the request body
func (p *Provider) buildRequestBodySchema() *jsonschema.Schema {
	if p.operation.RequestBody == nil || p.operation.RequestBody.Schema == nil {
		return nil
	}

	return p.operation.RequestBody.Schema
}

// Execute executes the tool operation
func (p *Provider) Execute(ctx context.Context, params *mcp.CallToolParamsRaw) (*mcp.CallToolResult, error) {
	// Build the request URL
	requestURL, err := p.buildRequestURL(params)
	if err != nil {
		return nil, err
	}

	// Build headers
	headers, err := p.buildHeaders(ctx, params)
	if err != nil {
		return nil, err
	}

	// Build request body
	var body io.Reader
	if p.operation.RequestBody != nil {
		var args map[string]interface{}
		if len(params.Arguments) > 0 {
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
			}
		} else {
			args = make(map[string]interface{})
		}

		if bodyData, exists := args[p.bodyName]; exists {
			bodyBytes, err := json.Marshal(bodyData)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			body = bytes.NewReader(bodyBytes)
			headers["content-type"] = p.operation.RequestBody.ContentType
		}

	}

	// Create HTTP request
	httpRequest, err := http.NewRequestWithContext(ctx, string(p.operation.Method), requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	for k, v := range headers {
		httpRequest.Header.Set(k, v)
	}

	// Execute request
	client := &http.Client{}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer response.Body.Close()

	// Read response
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	textContent := &mcp.TextContent{
		Text: string(responseBody),
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{textContent},
	}, nil
}

// buildRequestURL builds the complete request URL
func (p *Provider) buildRequestURL(params *mcp.CallToolParamsRaw) (string, error) {
	var args map[string]interface{}
	if len(params.Arguments) > 0 {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
		}
	} else {
		args = make(map[string]interface{})
	}

	// Determine base URL
	baseURL := p.endpoint
	if baseURL == "" {
		region, exists := args["serverRegion"]
		if !exists {
			return "", fmt.Errorf("no region specified and no endpoint configured")
		}

		regionStr, ok := region.(string)
		if !ok {
			return "", fmt.Errorf("region must be a string")
		}

		// Find the region server
		for _, rs := range p.regionServers {
			if rs.Region == regionStr {
				baseURL = rs.URL
				break
			}
		}

		if baseURL == "" {
			regions := make([]string, len(p.regionServers))
			for i, rs := range p.regionServers {
				regions[i] = rs.Region
			}
			return "", fmt.Errorf("invalid region %s. Available regions: %s", regionStr, strings.Join(regions, ", "))
		}
	}

	// Replace path parameters
	path := p.operation.Path
	pathParamRegex := regexp.MustCompile(`\{([^}]+)\}`)
	path = pathParamRegex.ReplaceAllStringFunc(path, func(match string) string {
		paramName := match[1 : len(match)-1] // Remove { and }
		if value, exists := args[paramName]; exists {
			return fmt.Sprintf("%v", value)
		}
		return match
	})

	// Build query parameters
	queryParams := url.Values{}

	// Add global query parameters
	for k, v := range p.globalQuery {
		queryParams.Set(k, v)
	}

	// Add default limit for operations that support it
	if p.hasLimitParameter() {
		if _, exists := queryParams["limit"]; !exists {
			queryParams.Set("limit", "10")
		}
	}

	// Add request-specific query parameters
	if queryArgs, exists := args["query"]; exists {
		if queryMap, ok := queryArgs.(map[string]interface{}); ok {
			for k, v := range queryMap {
				queryParams.Set(k, fmt.Sprintf("%v", v))
			}
		}
	}

	// Construct final URL
	fullURL := baseURL + path
	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	return fullURL, nil
}

// buildHeaders builds the request headers
func (p *Provider) buildHeaders(ctx context.Context, params *mcp.CallToolParamsRaw) (map[string]string, error) {
	headers := make(map[string]string)

	// Add global headers
	for k, v := range p.globalHeaders {
		headers[k] = v
	}

	// Add request-specific headers
	var args map[string]interface{}
	if len(params.Arguments) > 0 {
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}
	} else {
		args = make(map[string]interface{})
	}

	if headerArgs, exists := args["headers"]; exists {
		if headerMap, ok := headerArgs.(map[string]interface{}); ok {
			for k, v := range headerMap {
				headers[strings.ToLower(k)] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Add authorization header
	if p.authenticator.IsPassthrough() {
		// For passthrough auth, look for authorization header in request context
		if requestHeaders := ctx.Value("requestHeaders"); requestHeaders != nil {
			if httpHeaders, ok := requestHeaders.(http.Header); ok {
				auth := httpHeaders.Get("Authorization")
				if auth == "" {
					return nil, fmt.Errorf("Authorization header is required when using passthrough authentication mode. The client must provide the 'Authorization' header in the HTTP request")
				}
				headers["authorization"] = auth
			} else {
				return nil, fmt.Errorf("Authorization header is required when using passthrough authentication mode")
			}
		} else {
			return nil, fmt.Errorf("Authorization header is required when using passthrough authentication mode")
		}
	} else {
		token, err := p.authenticator.GetBearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get bearer token: %w", err)
		}
		headers["authorization"] = fmt.Sprintf("Bearer %s", token)
	}

	return headers, nil
}

// hasLimitParameter checks if the operation has a limit parameter
func (p *Provider) hasLimitParameter() bool {
	for _, param := range p.operation.Parameters {
		if param.In == "query" && param.Name == "limit" {
			return true
		}
	}
	return false
}

// hasRequiredFields checks if a schema has required fields
func hasRequiredFields(schema *jsonschema.Schema) bool {
	return schema != nil && len(schema.Required) > 0
}
