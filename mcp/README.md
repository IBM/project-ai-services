# AI Services MCP Sidecar - Developer Guide

## 📋 Table of Contents
- [Overview](#overview)
- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [Core Components](#core-components)
- [Authentication System](#authentication-system)
- [Transport Modes](#transport-modes)
- [Tool Generation Pipeline](#tool-generation-pipeline)
- [Running the Application](#running-the-application)
  - [Building the Project](#building-the-project)
  - [Running Locally](#running-locally)
  - [Running with Podman](#running-with-podman)
- [Testing](#testing)
- [Dependencies](#dependencies)

## Overview

The AI Services MCP Sidecar is a Go implementation of the Model Context Protocol (MCP) that dynamically generates tools from OpenAPI specifications. It runs as a second container inside each AI Services service pod, exposing the service's API as MCP tools that can be consumed by any MCP-compatible client.

### Key Features
- Dynamic tool generation from OpenAPI specs
- Multiple authentication methods (API Key, Token, Passthrough)
- Support for both stdio and HTTP transports
- Tag-based tool filtering
- Global query parameters and headers support
- TLS skip-verify for self-signed or internal-CA endpoints
- Rate limiting for HTTP transport (configurable via environment variables)
- MCP client configuration output (`--config`)

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                     MCP Client                           │
└────────────────────┬─────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────┐
│                    Transport Layer                       │
│         ┌──────────────┬────────────────┐                │
│         │ Stdio Server │  HTTP Server   │                │
│         └──────────────┴────────────────┘                │
└────────────────────┬─────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────┐
│                  Tool Aggregator                         │
│    ┌────────────────────────────────────────────┐        │
│    │ • Tool Registration                        │        │
│    │ • Schema Management                        │        │
│    │ • Request Routing                          │        │
│    └────────────────────────────────────────────┘        │
└────────────────────┬─────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────┐
│                 OpenAPI Interface                        │
│    ┌────────────────────────────────────────────┐        │
│    │ • Spec Parsing                             │        │
│    │ • Operation Extraction                     │        │
│    │ • Parameter Processing                     │        │
│    └────────────────────────────────────────────┘        │
└────────────────────┬─────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────┐
│                  AI Services APIs                        │
└──────────────────────────────────────────────────────────┘
```

### Sidecar Deployment

The sidecar runs as a second container inside each service pod — no new pod is created. Each service has a dedicated port:

| Service    | Sidecar Port |
|------------|-------------|
| chatbot    | 5001        |
| digitize   | 4002        |
| summarize  | 6001        |
| similarity | 7001        |

Caddy automatically registers an `<service>-mcp.<app-domain>` route for each sidecar when an application is created, and removes it when the application is deleted.

## Project Structure

```
mcp/
├── cmd/
│   └── ai-services-mcp/
│       └── main.go              # Application entry point & CLI
│
├── internal/
│   ├── authenticator/           # Authentication implementations
│   │   ├── interface.go         # Authenticator interface
│   │   ├── api_key.go           # API key authentication
│   │   ├── env.go               # Environment variable auth
│   │   ├── passthrough.go       # Passthrough authentication
│   │   └── token.go             # Direct token authentication
│   │
│   ├── config/                  # Configuration management
│   │   └── config.go            # MCP client config generation
│   │
│   ├── errors/                  # Custom error types
│   │   └── errors.go            # Error definitions
│   │
│   ├── openapi/                 # OpenAPI processing
│   │   ├── interface.go         # OpenAPI interface & operations
│   │   ├── loader.go            # Spec loading (file/URL)
│   │   └── convert.go           # Schema conversion to JSON Schema
│   │
│   ├── server/                  # MCP server implementations
│   │   ├── stdio.go             # Stdio transport server
│   │   └── http.go              # HTTP transport server
│   │
│   ├── tool/                    # Tool management
│   │   ├── aggregator.go        # Tool aggregation & routing
│   │   └── provider.go          # Tool execution provider
│   │
│   └── types/                   # Shared type definitions
│       └── types.go             # Common types & structs
│
├── go.mod                       # Go module dependencies
├── go.sum                       # Dependency checksums
└── Makefile                     # Build automation
```

## Core Components

### 1. Main Entry Point (`cmd/ai-services-mcp/main.go`)

The main package provides:
- **CLI Interface**: Uses Cobra for command-line argument parsing
- **Flag Validation**: Ensures proper authentication and configuration
- **Server Initialization**: Sets up either stdio or HTTP transport
- **Error Handling**: Provides detailed usage information on errors

**Key Functions:**
- `runServer()`: Main orchestration function
- `createAuthenticator()`: Factory for authentication methods

### 2. OpenAPI Interface (`internal/openapi/`)

Processes OpenAPI specifications into usable operations:

**interface.go:**
- `NewInterface()`: Creates interface from OpenAPI document
- `collectOperations()`: Extracts all API operations

**loader.go:**
- Handles both local file and remote URL loading
- Validates OpenAPI spec structure
- Handles schema reference resolution
- Supports OpenAPI 3.0+ specifications

**convert.go:**
- `ConvertSchemaToJSONSchema()`: Converts libopenapi schemas to JSON Schema format
- Enables proper MCP tool schema generation

### 3. Tool System (`internal/tool/`)

Manages tool generation and execution:

**aggregator.go:**
- `GetTools()`: Returns filtered tool list
- `HandleToolCall()`: Routes tool execution

**provider.go:**
- Handles individual tool execution
- Manages HTTP request construction
- Processes API responses
- Uses modular schema building with `schemaBuilder` helper
- Includes focused methods for different parameter types:
  - `addPathParametersToSchema()`: Processes path parameters
  - `addQueryParametersToSchema()`: Manages query parameters
  - `addHeaderParametersToSchema()`: Handles header parameters
  - `addRequestBodyToSchema()`: Adds request body schemas

### 4. Server Implementations (`internal/server/`)

**stdio.go (Default Transport):**
- Uses MCP SDK's stdio transport
- Handles graceful shutdown with signals
- Maintains persistent connection with client

**http.go (HTTP Transport):**
- Implements HTTP server with streamable transport
- Creates MCP server once at startup (maintains single instance)
- Includes CORS support for web clients
- Provides health check endpoint at `/health`

### 5. Authentication System (`internal/authenticator/`)

Supports multiple authentication methods:

**API Key** (`api_key.go`):
- Direct API key usage
- Exchanges key for IAM token

**Environment** (`env.go`):
- Reads API key from environment variable
- Format: `--auth-api-key $VAR_NAME`

**Token** (`token.go`):
- Direct token usage
- No token refresh capability

**Passthrough** (`passthrough.go`):
- Client provides Authorization header
- Forwarded verbatim to the upstream API, which validates it
- Required for HTTP transport, and only usable with HTTP transport

**Environment** note: `--auth-api-key` accepts `$VAR_NAME` syntax to read the API key from an
environment variable at startup (and re-reads it on each token refresh).

## Authentication System

The authentication system is designed with flexibility and security in mind:

```go
type Authenticator interface {
    GetBearerToken(ctx context.Context) (string, error)
    IsPassthrough() bool
    GetType() string
}
```

### Authentication Flow:

1. **Initialization**: CLI flags determine auth method
2. **Validation**: Ensures only one auth method is specified, and that it matches the transport
3. **Token Acquisition**: Different strategies per authenticator
4. **Request Enhancement**: Adds Authorization header to API calls

### Authentication and Transport

Auth mode and transport are not independent — each transport permits exactly one class of authenticator:

| Transport | Permitted auth | Rationale |
|-----------|----------------|-----------|
| Stdio (default) | `--auth-api-key`, `--auth-token` | The server is a subprocess of a single trusted client, so a server-held credential is scoped to that user. |
| HTTP (`--http`) | `--auth-passthrough` only | The server does not authenticate incoming requests, so a server-held credential would be usable by any caller that can reach the port. |

Any other combination is rejected at startup. In particular, `--auth-passthrough` cannot be used with
stdio: there is no inbound HTTP request to take an Authorization header from, so every tool call
would fail.

### Security Considerations:
- Tokens are cached where appropriate
- Automatic token refresh for API key auth
- No credentials stored in memory longer than necessary
- HTTP transport never holds a credential of its own; each caller is authorized individually by the upstream API

## Transport Modes

### Stdio Transport (Default)
- Designed for desktop MCP clients
- Persistent bidirectional communication
- Maintains session state
- Authenticates with a server-held credential (API key or token)

### HTTP Transport
- RESTful API interface
- CORS-enabled for web clients
- Session management via `Mcp-Session-Id` header
- Requires `--auth-passthrough`: each caller supplies its own Authorization header, which is
  forwarded to the upstream API for validation. The server holds no credential, so one instance
  can serve many users without any of them borrowing another's access.
- Rate limiting per client IP (X-Forwarded-For aware), configurable via:
  - `RATE_LIMIT_REQUESTS` — number of requests allowed per window (default: `20`)
  - `RATE_LIMIT_PER_SECONDS` — window size in seconds (default: `60`)

## Tool Generation Pipeline

The tool generation follows this pipeline:

1. **OpenAPI Loading** - Fetch and parse specification
2. **Operation Extraction** - Identify all API operations
3. **Tag Filtering** - Filter operations by `--tag` flag (e.g. `--tag similarity`)
4. **Parameter Analysis** - Process path/query/body parameters
5. **Tool Creation** - Generate MCP tool definitions
6. **Handler Registration** - Map tools to execution handlers

## Running the Application

### Building the Project

```bash
# Install/update dependencies
make install

# Build the Go binary (native)
make build-binary

# Build the container image
make build

# Run the application with --help
make run

# Run linter
make lint

# Cross-compile for all supported platforms (darwin/amd64, darwin/arm64, linux/amd64, linux/ppc64le)
make cross-compile
```

### Running Locally

The sidecar needs an OpenAPI spec URL (`--description`) and the service base URL (`--endpoint`).
In the pod templates these are wired to the service's own `/openapi.json` endpoint via Caddy.

The default HTTP port is **3000**. The examples below use `-p 7001` to match the similarity sidecar port assignment.

To output an MCP client-compatible configuration instead of starting the server, use `--config` (`-C`):

```bash
./bin/ai-services-mcp \
  --description https://<service-url>/openapi.json \
  --endpoint https://<service-url> \
  --auth-api-key your-api-key-here \
  --config
```

For endpoints with self-signed or internal-CA certificates, add `--tls-skip-verify`:

```bash
./bin/ai-services-mcp \
  --description https://<service-url>/openapi.json \
  --endpoint https://<service-url> \
  --auth-api-key your-api-key-here \
  --tls-skip-verify \
  --tag similarity
```

**HTTP mode (used by the sidecar in pods):**

```bash
./bin/ai-services-mcp \
  --description https://similarity-mcp-<id>.<domain>/openapi.json \
  --endpoint https://similarity-mcp-<id>.<domain> \
  --auth-passthrough \
  --tag similarity \
  --http \
  -p 7001
```

Callers must supply their own Authorization header on each request:

```bash
# Initialize session
SESSION=$(curl -ski http://localhost:7001/mcp \
  -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' \
  2>&1 | grep -i "mcp-session-id" | awk '{print $2}' | tr -d '\r')

# List tools
curl -s http://localhost:7001/mcp \
  -X POST -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

**Stdio mode (for local MCP clients):**

```bash
./bin/ai-services-mcp \
  --description https://<service-url>/openapi.json \
  --endpoint https://<service-url> \
  --auth-api-key your-api-key-here \
  --tag similarity
```

### Running with Podman

The project includes a multi-stage Containerfile optimized for size and security.

#### Building the Container Image

```bash
# Build the image (defaults to icr.io/ai-services-private/ai-services-mcp:v0.0.1)
make build

# Build targeting the cicd registry
make build REGISTRY=icr.io/ai-services-cicd

# Push the image
make push REGISTRY=icr.io/ai-services-cicd
```

#### Running the Container

HTTP mode requires `--auth-passthrough`, so the container never needs a credential of its own —
each caller supplies its own Authorization header.

**HTTP Mode (matches sidecar pod configuration):**

```bash
podman run -p 7001:7001 \
  icr.io/ai-services-cicd/ai-services-mcp:v0.0.1 \
  --description https://<service-url>/openapi.json \
  --endpoint https://<service-url> \
  --auth-passthrough \
  --tag similarity \
  --http \
  -p 7001
```

**With Local OpenAPI Specification:**

```bash
podman run -p 7001:7001 \
  -v /path/to/specs:/app/specs:ro \
  icr.io/ai-services-cicd/ai-services-mcp:v0.0.1 \
  --description /app/specs/openapi.json \
  --endpoint https://<service-url> \
  --auth-passthrough \
  --http \
  -p 7001
```

#### Stdio Mode in Podman

Stdio transport is where server-held credentials belong: the container is a subprocess of a single
trusted client, so run it with `-i` and no published port.

```bash
podman run -i --rm \
  icr.io/ai-services-cicd/ai-services-mcp:v0.0.1 \
  --description https://<service-url>/openapi.json \
  --endpoint https://<service-url> \
  --auth-api-key your-api-key-here \
  --tag similarity
```

#### Podman Image Details

- **Base Image**: UBI minimal (Red Hat Universal Base Image) — runtime stage uses `ubi9/ubi-minimal`
- **User**: Runs as non-root (UID 1001)
- **Security**: Minimal attack surface, no unnecessary packages
- **Container architecture**: `linux/ppc64le` (built by the Containerfile; the Go binary can be cross-compiled for `linux/amd64` via `make cross-compile`)

#### Health Check

```bash
# Check container health via HTTP (requires --http flag)
curl http://localhost:<port>/health

# Check container status
podman ps
```

## Testing

The project maintains comprehensive test coverage across all major components.

### Testing Philosophy

- **Mock-Based Testing**: External dependencies (HTTP clients, file systems, environment variables) are mocked to ensure reliable, fast test execution
- **Business Logic Focus**: Tests validate core functionality without requiring external services
- **Table-Driven Tests**: Consistent test patterns using Go's table-driven test approach
- **Comprehensive Coverage**: Each package includes unit tests covering success paths, error conditions, and edge cases

### Test Architecture

The testing strategy is organized into three phases:

#### Foundation Tests
Core utilities and data structures:
- **`internal/types`** - Type definitions and validation
- **`internal/errors`** - Custom error handling
- **`internal/config`** - MCP client configuration generation
- **`internal/openapi`** - OpenAPI parsing and conversion

#### Core Functionality Tests
Business logic and tool system:
- **`internal/authenticator`** - All authentication methods (API Key, Token, Environment, Passthrough)
- **`internal/tool`** - Tool aggregation, provider execution, schema building

#### Application Tests
Server and application entry points:
- **`internal/server`** - HTTP and STDIO server implementations
- **`cmd/ai-services-mcp`** - Main application, CLI validation, flag parsing

### Running Tests

```bash
# Run all tests
make test

# Run tests with detailed coverage report by package
make test-coverage

# Generate HTML coverage report (opens coverage.html)
make test-coverage-html

# Clean coverage files
make clean-coverage
```

## Dependencies

Key dependencies from `go.mod`:
- `github.com/modelcontextprotocol/go-sdk`: MCP protocol implementation
- `github.com/pb33f/libopenapi`: OpenAPI parsing and processing
- `github.com/IBM/go-sdk-core`: IAM token exchange for API key authentication
- `github.com/spf13/cobra`: CLI framework
- `github.com/google/jsonschema-go`: JSON Schema generation
- `golang.org/x/time`: Token-bucket rate limiter for HTTP transport
