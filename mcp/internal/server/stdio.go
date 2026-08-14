package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/project-ai-services/mcp/internal/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// StartStdioServer starts the stdio MCP server using the official MCP SDK
func StartStdioServer(aggregator *tool.Aggregator, tags []string) error {
	server := &StdioServer{
		aggregator: aggregator,
		tags:       tags,
	}

	return server.Start()
}

// StdioServer implements the stdio MCP transport using the official SDK
type StdioServer struct {
	aggregator *tool.Aggregator
	tags       []string
}

// Start starts the stdio server using the MCP SDK
func (s *StdioServer) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create server implementation
	impl := &mcp.Implementation{
		Name:    s.aggregator.GetFriendlyName(),
		Version: "1.0.0",
	}

	// Create MCP server
	mcpServer := mcp.NewServer(impl, &mcp.ServerOptions{})

	// Register tools from aggregator
	tools := s.aggregator.GetTools(s.tags)
	// Create a single tool handler that delegates to the aggregator
	handler := s.createToolHandler()

	for _, tool := range tools {
		// Add the tool to the MCP server with explicit types
		mcpServer.AddTool(tool, handler)
	}

	// Create stdio transport
	transport := &mcp.StdioTransport{}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	// Start the server
	return mcpServer.Run(ctx, transport)
}

// createToolHandler creates an MCP tool handler that delegates to the aggregator
func (s *StdioServer) createToolHandler() mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Convert to the aggregator's expected type
		genericParams := &mcp.CallToolParamsRaw{
			Meta:      request.Params.Meta,
			Name:      request.Params.Name,
			Arguments: request.Params.Arguments,
		}

		// Call the aggregator directly with the MCP SDK types
		result, err := s.aggregator.HandleToolCall(ctx, genericParams)
		if err != nil {
			return nil, err
		}

		return result, nil
	}
}
