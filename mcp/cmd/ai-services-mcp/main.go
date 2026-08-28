package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/mcp/internal/authenticator"
	"github.com/project-ai-services/mcp/internal/config"
	"github.com/project-ai-services/mcp/internal/errors"
	"github.com/project-ai-services/mcp/internal/openapi"
	"github.com/project-ai-services/mcp/internal/server"
	"github.com/project-ai-services/mcp/internal/tool"
	"github.com/spf13/cobra"
)

var (
	description     string
	endpoint        string
	authAPIKey      string
	authToken       string
	authPassthrough bool
	queries         []string
	headers         []string
	tags            []string
	configOutput    bool
	httpMode        bool
	port            int
	tlsSkipVerify   bool
)

var rootCmd = &cobra.Command{
	Use:   "ai-services-mcp",
	Short: "AI Services MCP Server",
	Long: `An MCP (Model Context Protocol) server that dynamically generates tools from AI Services OpenAPI specifications.

This server loads OpenAPI specifications for AI Services and creates MCP tools that can be used by Claude and other MCP clients to interact with AI Services APIs.`,
	RunE: runServer,
}

func init() {
	rootCmd.Flags().StringVarP(&description, "description", "d", "", "The local OpenAPI description file path or remote URL to use (required)")
	rootCmd.Flags().StringVarP(&endpoint, "endpoint", "e", "", "The service endpoint URL to use")
	rootCmd.Flags().StringVarP(&authAPIKey, "auth-api-key", "k", "", "AI Services API key, environment variable ($VAR)")
	rootCmd.Flags().StringVarP(&authToken, "auth-token", "a", "", "IAM token to use for authentication")
	rootCmd.Flags().BoolVarP(&authPassthrough, "auth-passthrough", "P", false, "Use passthrough authentication mode")
	rootCmd.Flags().StringSliceVarP(&queries, "query", "Q", nil, "Global query parameter in key=value format")
	rootCmd.Flags().StringSliceVarP(&headers, "header", "H", nil, "Global header in key=value format")
	rootCmd.Flags().StringSliceVarP(&tags, "tag", "T", nil, "Only expose tools for operations with specified tags")
	rootCmd.Flags().BoolVarP(&configOutput, "config", "C", false, "Output MCP client-compatible configuration instead of starting server")
	rootCmd.Flags().BoolVarP(&httpMode, "http", "S", false, "Use HTTP transport instead of stdio")
	rootCmd.Flags().IntVarP(&port, "port", "p", 3000, "Port number for HTTP server (used with --http)")
	rootCmd.Flags().BoolVar(&tlsSkipVerify, "tls-skip-verify", false, "Skip TLS certificate verification for the description fetch and API requests (insecure; for self-signed or internal-CA endpoints)")

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

	// Validate endpoint format if provided
	if endpoint != "" {
		if err := validateEndpoint(endpoint); err != nil {
			return err
		}
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
	doc, err := openapi.LoadDescription(description, tlsSkipVerify)
	if err != nil {
		return fmt.Errorf("failed to load OpenAPI description: %w", err)
	}

	// Create interface
	intf := openapi.NewInterface(doc)

	// Create tool aggregator
	aggregator, err := tool.NewAggregator(intf, endpoint, auth, globalQuery, globalHeaders, tlsSkipVerify)
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

	// Start the appropriate server
	if httpMode {
		// Create simple implementations for dependencies
		logger := &server.StdLogger{}
		signalHandler := &server.OSSignalHandler{}
		limit, burst, err := server.GetRateLimiterConfig()
		if err != nil {
			return fmt.Errorf("failed to configure rate limiter: %v", err)
		}

		rateLimiter := server.NewRateLimiterManager(limit, burst)

		// Create HTTP server with dependency injection
		httpServer := server.NewHTTPServer(
			port,
			aggregator,
			tags,
			logger,
			signalHandler,
			rateLimiter,
		)

		return httpServer.Start()
	} else {
		return server.StartStdioServer(aggregator, tags)
	}
}

func validateEndpoint(endpoint string) error {
	// Allow http://localhost for local testing
	if strings.HasPrefix(endpoint, "http://localhost") {
		return nil
	}

	// Validate HTTPS
	if !strings.HasPrefix(endpoint, "https://") {
		return errors.NewUsageError("Invalid endpoint: %s. Must use HTTPS protocol", endpoint)
	}

	return nil
}

func createAuthenticator() (authenticator.Authenticator, error) {
	authCount := 0
	if authAPIKey != "" {
		authCount++
	}
	if authToken != "" {
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

	// The HTTP server does not authenticate incoming requests: authorization is
	// delegated to the upstream API, which validates the caller's own token. A
	// server-held credential would therefore be usable by anyone who can reach
	// the port, so HTTP transport and passthrough authentication require each
	// other. Stdio has no request headers to pass through.
	if httpMode && !authPassthrough {
		return nil, errors.NewUsageError(
			"Must use --auth-passthrough with --http. The HTTP server does not authenticate " +
				"incoming requests, so a server-held credential would be usable by any caller " +
				"that can reach the port")
	}
	if authPassthrough && !httpMode {
		return nil, errors.NewUsageError(
			"Must use --http with --auth-passthrough. Stdio transport has no request headers " +
				"to pass through")
	}

	if authAPIKey != "" {
		if strings.HasPrefix(authAPIKey, "$") {
			return authenticator.NewEnvAuthenticator(authAPIKey[1:])
		} else {
			return authenticator.NewAPIKeyAuthenticator(authAPIKey), nil
		}
	}

	if authToken != "" {
		return authenticator.NewTokenAuthenticator(authToken), nil
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
  -k, --auth-api-key    <key> The AI Services API key with which to obtain
                              tokens to authenticate requests. Cannot be used
                              with --auth-token or --http.
                       $<VAR> As above, but read in the API key from an
                              environment variable. Note that this works
                              when a literal $-prefixed variable name is
                              passed in outside of a shell context. Cannot be
                              used with --auth-token or --http.
  -a, --auth-token     <token> The IAM token with which to authenticate
                              requests. Cannot be used with --auth-api-key
                              or --http.
  -P, --auth-passthrough      Use passthrough authentication mode where the
                              client provides the authorization header in each
                              request. Cannot be used with other auth options.
                              Requires --http, and is required by --http.
  -Q, --query   <key>=<value> A query parameter value to include with every
                              request. Required when the API has globally
                              required query parameters. Can be used multiple
                              times.
  -H, --header  <key>=<value> A header value to include with every request
                              Required when the API has globally required
                              request headers. Can be used multiple times.
  -T, --tag <tag name>        Only expose tools for operations with one of the
                              provided tags. Can be used multiple times.
  -S, --http                  Use HTTP transport instead of stdio. Starts an
                              HTTP server with MCP Streamable HTTP transport.
                              Requires --auth-passthrough, as the server does
                              not authenticate incoming requests.
  -p, --port           <port> Port number for HTTP server (default: 3000).
                              Only used with --http flag.
  --tls-skip-verify           Skip TLS certificate verification when fetching
                              the OpenAPI description and when calling the
                              service endpoint. Insecure; only for endpoints
                              with self-signed or internal-CA certificates.
  -C, --config                Instead of starting an MCP server, output an
                              MCP client-compatible configuration.
  --help                      Show this usage information.

Transport Modes:
  Default: Stdio transport (for use with MCP clients like Claude Desktop).
           Authenticates with a server-held credential: --auth-api-key
           or --auth-token.
  --http:  HTTP transport (for direct HTTP clients or web interfaces).
           Requires --auth-passthrough: each caller supplies its own
           Authorization header, which is forwarded to the API for
           validation. The server holds no credential of its own.`
}
