# ============================================================================
# Build Stage
# ============================================================================
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies (cached if go.mod/go.sum unchanged)
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with optimizations
# -ldflags="-s -w" strips debug info and reduces binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" \
    -o /build/bin/ibmcloud-api-mcp \
    ./cmd/ibmcloud-api-mcp/main.go

# ============================================================================
# Runtime Stage
# ============================================================================
FROM alpine:latest

# Create non-root user for security
RUN addgroup -g 1000 mcpuser && \
    adduser -D -u 1000 -G mcpuser mcpuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/bin/ibmcloud-api-mcp /app/ibmcloud-api-mcp

# Change ownership to non-root user
RUN chown -R mcpuser:mcpuser /app

# Switch to non-root user
USER mcpuser

# Expose HTTP port (default: 3000)
EXPOSE 3000

# Set environment variables
ENV PORT=3000 \
    IAM_ENDPOINT=https://iam.cloud.ibm.com/identity/keys

# Health check (optional - adjust path based on your health endpoint)
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health || exit 1

# Set entrypoint to the binary
ENTRYPOINT ["/app/ibmcloud-api-mcp"]

# Default command - can be overridden at runtime
# Example: docker run ... ibmcloud-api-mcp --description <url> --endpoint <url> --auth-api-key <key> --http
CMD ["--help"]