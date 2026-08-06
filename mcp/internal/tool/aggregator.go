package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services-mcp/internal/authenticator"
	"github.com/project-ai-services/ai-services-mcp/internal/openapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Aggregator aggregates tools from OpenAPI operations
type Aggregator struct {
	intf          *openapi.Interface
	endpoint      string
	authenticator authenticator.Authenticator
	globalQuery   map[string]string
	globalHeaders map[string]string
	providers     []*Provider
}

// NewAggregator creates a new tool aggregator
func NewAggregator(intf *openapi.Interface, endpoint string, auth authenticator.Authenticator,
	globalQuery, globalHeaders map[string]string) (*Aggregator, error) {

	aggregator := &Aggregator{
		intf:          intf,
		endpoint:      endpoint,
		authenticator: auth,
		globalQuery:   globalQuery,
		globalHeaders: canonicalizeHeaders(globalHeaders),
		providers:     make([]*Provider, 0),
	}

	// Create providers for each operation
	for _, operation := range intf.Operations {
		provider, err := NewProvider(operation, endpoint,
			auth, globalQuery, aggregator.globalHeaders)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider for operation %s: %w", operation.OperationID, err)
		}
		aggregator.providers = append(aggregator.providers, provider)
	}

	return aggregator, nil
}

// GetTools returns all tools, optionally filtered by tags
func (a *Aggregator) GetTools(tags []string) []*mcp.Tool {
	var tools []*mcp.Tool

	// Filter providers by tags if specified
	providers := a.providers
	if len(tags) > 0 {
		providers = make([]*Provider, 0)
		tagSet := make(map[string]bool)

		// Convert tags to a set for faster lookup
		for _, tag := range tags {
			// Handle comma-separated tags
			for _, t := range strings.Split(tag, ",") {
				tagSet[strings.TrimSpace(t)] = true
			}
		}

		for _, provider := range a.providers {
			// Check if provider has any of the requested tags
			hasTag := false
			for _, providerTag := range provider.operation.Tags {
				if tagSet[providerTag] {
					hasTag = true
					break
				}
			}
			if hasTag {
				providers = append(providers, provider)
			}
		}
	}

	// Add provider tools
	for _, provider := range providers {
		tools = append(tools, provider.GetTool())
	}

	return tools
}

// HandleToolCall handles an MCP tool call request
func (a *Aggregator) HandleToolCall(ctx context.Context, params *mcp.CallToolParamsRaw) (*mcp.CallToolResult, error) {
	// Find the provider for this tool
	for _, provider := range a.providers {
		if provider.operation.OperationID == params.Name {
			return provider.Execute(ctx, params)
		}
	}

	return nil, fmt.Errorf("unknown tool: %s", params.Name)
}

// GetFriendlyName returns the friendly name for the service
func (a *Aggregator) GetFriendlyName() string {
	return a.intf.Doc.Info.Title
}

// GetName returns the canonical name for the service
func (a *Aggregator) GetName() string {
	return a.intf.Name
}

// GetTags returns all available tags
func (a *Aggregator) GetTags() []string {
	return a.intf.Tags
}

// canonicalizeHeaders converts headers to lowercase keys
func canonicalizeHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}

	canonical := make(map[string]string)
	for k, v := range headers {
		canonical[strings.ToLower(k)] = v
	}
	return canonical
}
