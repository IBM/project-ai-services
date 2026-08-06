package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services-mcp/internal/authenticator"
	"github.com/project-ai-services/ai-services-mcp/internal/config"
	"github.com/project-ai-services/ai-services-mcp/internal/errors"
	"github.com/project-ai-services/ai-services-mcp/internal/openapi"
	"github.com/project-ai-services/ai-services-mcp/internal/server"
	"github.com/project-ai-services/ai-services-mcp/internal/tool"
	"github.com/spf13/cobra"
)

var (
	description     string
	endpoint        string
	authAPIKey      string
	authPassthrough bool
	queries         []string
	headers         []string
	tags            []string
	configOutput    bool
	port            int
)

var rootCmd = &cobra.Command{
	Use:   "ai-services-mcp",
	Short: "AI Services MCP Server",
	Long: `An MCP (Model Context Protocol) server that dynamically generates tools from OpenAPI specifications.

This server loads OpenAPI specifications for AI Services and creates MCP tools that can be used by AI agents to interact with the services.`,
	RunE: runServer,
}

func init() {
	rootCmd.Flags().StringVarP(&description, "description", "d", "", "The local OpenAPI description file path or remote URL to use (required)")
	rootCmd.Flags().StringVarP(&endpoint, "endpoint", "e", "", "The service endpoint URL to use")
	rootCmd.Flags().StringVarP(&authAPIKey, "auth-api-key", "k", "", "API key or environment variable ($VAR)")
	rootCmd.Flags().BoolVarP(&authPassthrough, "auth-passthrough", "P", false, "Use passthrough authentication mode")
	rootCmd.Flags().StringSliceVarP(&queries, "query", "Q", nil, "Global query parameter in key=value format")
	rootCmd.Flags().StringSliceVarP(&headers, "header", "H", nil, "Global header in key=value format")
	rootCmd.Flags().StringSliceVarP(&tags, "tag", "T", nil, "Only expose tools for operations with specified tags")
	rootCmd.Flags().BoolVarP(&configOutput, "config", "C", false, "Output MCP client-compatible configuration instead of starting server")
	rootCmd.Flags().IntVarP(&port, "port", "p", 3000, "Port number for HTTP server")

	if err := rootCmd.MarkFlagRequired("description"); err != nil {
		panic(err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		handleError(err)
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	// Validate that no positional arguments are provided
	if len(args) > 0 {
		return errors.NewUsageError("Must not use positional arguments. Got: %s", fmt.Sprintf("%v", args))
	}

	// Validate and create authenticator
	auth, err := createAuthenticator()
	if err != nil {
		return err
	}

	// Parse global parameters
	globalQuery, err := parseKeyValuePairs(queries, "query parameter")
	if err != nil {
		return err
	}

	globalHeaders, err := parseKeyValuePairs(headers, "header")
	if err != nil {
		return err
	}

	// Load and parse OpenAPI description
	doc, err := openapi.LoadDescription(description)
	if err != nil {
		return fmt.Errorf("failed to load OpenAPI description: %w", err)
	}

	// Create interface
	intf := openapi.NewInterface(doc)

	// Create tool aggregator
	aggregator, err := tool.NewAggregator(intf, endpoint, auth, globalQuery, globalHeaders)
	if err != nil {
		return fmt.Errorf("failed to create tool aggregator: %w", err)
	}

	// Handle config output
	if configOutput {
		return outputConfig(aggregator.GetName())
	}

	// Validate tags if provided
	if len(tags) > 0 {
		if err := validateTags(tags, aggregator.GetTags()); err != nil {
			return err
		}
	}

	// Start HTTP server
	logger := &server.StdLogger{}
	signalHandler := &server.OSSignalHandler{}
	limit, burst, err := server.GetRateLimiterConfig()
	if err != nil {
		return fmt.Errorf("failed to configure rate limiter: %v", err)
	}

	rateLimiter := server.NewRateLimiterManager(limit, burst)

	httpServer := server.NewHTTPServer(
		port,
		aggregator,
		tags,
		logger,
		signalHandler,
		rateLimiter,
	)

	return httpServer.Start()
}

func createAuthenticator() (authenticator.Authenticator, error) {
	authCount := 0
	if authAPIKey != "" {
		authCount++
	}
	if authPassthrough {
		authCount++
	}

	if authCount == 0 {
		return nil, errors.NewUsageError("Must provide an authentication option")
	}
	if authCount > 1 {
		return nil, errors.NewUsageError("Must not use more than one authentication option")
	}

	if authAPIKey != "" {
		if strings.HasPrefix(authAPIKey, "$") {
			return authenticator.NewEnvAuthenticator(authAPIKey[1:])
		}
		return authenticator.NewAPIKeyAuthenticator(authAPIKey), nil
	}

	if authPassthrough {
		return authenticator.NewPassthroughAuthenticator(), nil
	}

	return nil, errors.NewUsageError("Invalid authentication configuration")
}

func parseKeyValuePairs(pairs []string, pairType string) (map[string]string, error) {
	result := make(map[string]string)

	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) < 2 {
			return nil, errors.NewUsageError("Must provide %s value in the form: <name>=<value>", pairType)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		result[key] = value
	}

	return result, nil
}

func outputConfig(serverName string) error {
	cfg, err := config.GenerateMCPClientConfig(serverName)
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	configStr, err := config.FormatMCPClientConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to format config: %w", err)
	}

	fmt.Println(configStr)
	return nil
}

func validateTags(requestedTags []string, availableTags []string) error {
	available := make(map[string]bool)
	for _, tag := range availableTags {
		available[tag] = true
	}

	var unknownTags []string
	for _, tag := range requestedTags {
		// Support comma-separated tags
		for _, t := range strings.Split(tag, ",") {
			t = strings.TrimSpace(t)
			if !available[t] {
				unknownTags = append(unknownTags, t)
			}
		}
	}

	if len(unknownTags) > 0 {
		return fmt.Errorf("tag(s) not found: %s\n\nAvailable tags: %s",
			strings.Join(unknownTags, ", "), strings.Join(availableTags, ", "))
	}

	return nil
}

func handleError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)

	if _, isUsageError := err.(*errors.UsageError); isUsageError {
		fmt.Fprintf(os.Stderr, "\n%s\n", getUsage())
	}
}

func getUsage() string {
	return `Usage: ai-services-mcp -d <API description> -e <service endpoint>

Flags:
  -d, --description    <path> The local OpenAPI description to use.
                        <URL> The remote OpenAPI description to use.
  -e, --endpoint        <URL> The service endpoint to use.
  -k, --auth-api-key    <key> API key for authentication.
                       $<VAR> Read the API key from an environment variable.
  -P, --auth-passthrough      Use passthrough authentication mode where the
                              client provides the authorization header in each
                              request. Cannot be used with other auth options.
  -Q, --query   <key>=<value> A query parameter value to include with every
                              request. Can be used multiple times.
  -H, --header  <key>=<value> A header value to include with every request.
                              Can be used multiple times.
  -T, --tag <tag name>        Only expose tools for operations with one of the
                              provided tags. Can be used multiple times.
  -p, --port           <port> Port number for HTTP server (default: 3000).
  -C, --config                Instead of starting an MCP server, output an
                              MCP client-compatible configuration.
  --help                      Show this usage information.`
}
