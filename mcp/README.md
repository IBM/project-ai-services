# IBM Cloud API MCP Server - Developer Guide

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
  - [Running with Docker](#running-with-docker)
- [Testing](#testing)
- [Dependencies](#dependencies)

## Overview

The IBM Cloud API MCP Server is a Go implementation of the Model Context Protocol (MCP) that dynamically generates tools from IBM Cloud OpenAPI specifications. It enables MCP clients to interact with IBM Cloud services through a standardized interface.

### Key Features
- Dynamic tool generation from OpenAPI specs
- Multiple authentication methods (API Key, Token, Passthrough)
- Support for both stdio and HTTP transports
- Schema inspection capabilities
- Tag-based tool filtering
- Global query parameters and headers support

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
│                  IBM Cloud Services                      │
└──────────────────────────────────────────────────────────┘
```

## Project Structure

```
go-api-mcp/
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
- `validateEndpoint()`: Ensures IBM Cloud endpoint format

### 2. OpenAPI Interface (`internal/openapi/`)

Processes OpenAPI specifications into usable operations:

**interface.go:**
- `NewInterface()`: Creates interface from OpenAPI document
- `collectOperations()`: Extracts all API operations
- `extractRegionServers()`: Parses regional endpoints

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
  - `addServerRegionToSchema()`: Handles region parameters
  - `addPathParametersToSchema()`: Processes path parameters
  - `addQueryParametersToSchema()`: Manages query parameters
  - `addHeaderParametersToSchema()`: Handles header parameters
  - `addRequestBodyToSchema()`: Adds request body schemas
- Pure helper functions:
  - `extractRegions()`: Extracts region names
  - `buildServerRegionSchema()`: Creates region selection schemas

### 4. Server Implementations (`internal/server/`)

**stdio.go (Default Transport):**
- Uses MCP SDK's stdio transport
- Handles graceful shutdown with signals
- Maintains persistent connection with client

**http.go (HTTP Transport):**
- Implements HTTP server with streamable transport
- Creates MCP server once at startup (maintains single instance)
- Includes CORS support for web clients
- Provides health check endpoint

### 5. Authentication System (`internal/authenticator/`)

Supports multiple authentication methods:

**API Key** (`api_key.go`):
- Direct API key usage
- Exchanges key for IAM token

**Environment** (`env.go`):
- Reads API key from environment variable
- Format: `--auth-api-key $VAR_NAME`

**Token** (`token.go`):
- Direct IAM token usage
- No token refresh capability

**Passthrough** (`passthrough.go`):
- Client provides Authorization header
- Forwarded verbatim to the upstream API, which validates it
- Required for HTTP transport, and only usable with HTTP transport

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
- Session management via headers
- Requires `--auth-passthrough`: each caller supplies its own Authorization header, which is
  forwarded to the upstream API for validation. The server holds no credential, so one instance
  can serve many users without any of them borrowing another's access.

## Tool Generation Pipeline

The tool generation follows this pipeline:

1. **OpenAPI Loading** - Fetch and parse specification
2. **Operation Extraction** - Identify all API operations
3. **Parameter Analysis** - Process path/query/body parameters
4. **Tool Creation** - Generate MCP tool definitions
5. **Handler Registration** - Map tools to execution handlers

## Running the Application

### Building the Project

```bash
# Install/update dependencies
make install

# Build the binary
make build

# Run the application with --help
make run
```

### Running Locally

```bash
# HTTP mode (requires --auth-passthrough)
./bin/ai-services-mcp \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-passthrough \
  --query version=2025-07-01 \
  --http \
  -p 3001
```

Callers must supply their own IAM token on each request:

```bash
curl -X POST http://localhost:3001/mcp \
  -H "Authorization: Bearer $(ibmcloud iam oauth-tokens --output json | jq -r .iam_token)" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

```bash
# STDIO mode (server holds the credential)
./bin/ai-services-mcp \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-token your-token-here \
  --query version=2025-07-01
```

### Running with Docker

The project includes a multi-stage Dockerfile optimized for size and security.

#### Building the Docker Image

```bash
# Build the image
docker build -t ai-services-mcp:latest .

# Build with a specific tag
docker build -t ai-services-mcp:v1.0.0 .
```

#### Running the Container

HTTP mode requires `--auth-passthrough`, so the container never needs a credential of its own —
each caller supplies its own Authorization header. The server-held credential options
(`--auth-api-key`, `--auth-token`) are stdio-only; see
[Stdio Mode in Docker](#stdio-mode-in-docker) below.

**Basic HTTP Mode (Recommended for Docker)**

```bash
docker run -p 3000:3000 \
  ai-services-mcp:latest \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-passthrough \
  --query version=2025-07-01 \
  --http
```

**With Local OpenAPI Specification**

```bash
# Mount local spec file
docker run -p 3000:3000 \
  -v /path/to/specs:/app/specs:ro \
  ai-services-mcp:latest \
  --description /app/specs/openapi.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-passthrough \
  --query version=2025-07-01 \
  --http
```

**Custom Port Configuration**

```bash
# Run on custom port (e.g., 8080)
docker run -p 8080:8080 \
  -e PORT=8080 \
  ai-services-mcp:latest \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-passthrough \
  --query version=2025-07-01 \
  --http \
  -p 8080
```

#### Stdio Mode in Docker

Stdio transport is where server-held credentials belong: the container is a subprocess of a single
trusted client, so run it with `-i` and no published port.

**Using Environment Variable for API Key**

```bash
# Set API key in environment
export IBMCLOUD_API_KEY=your-api-key-here

# Run container
docker run -i --rm \
  -e IBMCLOUD_API_KEY \
  ai-services-mcp:latest \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-api-key '$IBMCLOUD_API_KEY' \
  --query version=2025-07-01
```

**Using Direct Token Authentication**

```bash
docker run -i --rm \
  ai-services-mcp:latest \
  --description https://cloud.ibm.com/apidocs/codeengine/v2.json \
  --endpoint https://api.us-south.codeengine.cloud.ibm.com/v2 \
  --auth-token your-iam-token-here \
  --query version=2025-07-01
```

#### Docker Environment Variables

The following environment variables can be configured:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `3000` |
| `IAM_ENDPOINT` | IBM Cloud IAM endpoint | `https://iam.cloud.ibm.com/identity/keys` |
| `IBMCLOUD_API_KEY` | IBM Cloud API key (for convenience) | - |
| `RATE_LIMIT_REQUESTS` | Rate limit (requests per second) | System default |
| `RATE_LIMIT_PER_SECONDS` | Rate burst limit | System default |

#### Health Check

The container includes a health check that monitors the `/health` endpoint:

```bash
# Check container health
docker ps

# View health check logs
docker inspect --format='{{json .State.Health}}' <container-id>
```

#### Docker Image Details

- **Base Image**: Alpine Linux (minimal footprint)
- **Size**: ~20-30 MB (optimized multi-stage build)
- **User**: Runs as non-root user `mcpuser` (UID 1000)
- **Security**: Minimal attack surface, no unnecessary packages
- **Architecture**: Built for linux/amd64

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
- `github.com/IBM/go-sdk-core`: IBM Cloud SDK utilities
- `github.com/spf13/cobra`: CLI framework
