.PHONY: build clean clean-coverage test test-coverage test-coverage-html run install cross-compile help

# Build the application
build:
	go build -o bin/ibmcloud-api-mcp cmd/ibmcloud-api-mcp/main.go

# Build with optimizations
build-optimized:
	go build -ldflags="-s -w" -o bin/ibmcloud-api-mcp cmd/ibmcloud-api-mcp/main.go

# Clean built artifacts
clean:
	rm -f bin/ibmcloud-api-mcp bin/ibmcloud-api-mcp-*

# Clean test coverage files
clean-coverage:
	rm -f coverage.out coverage.html

# Run tests
test:
	go test ./...

# Run tests with coverage by package and overall summary
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@echo ""
	@echo "Overall Coverage:"
	@echo "================"
	@go tool cover -func=coverage.out | grep "total:" | awk '{print "Total Coverage: " $$3}'

# Generate HTML coverage report and open in browser
test-coverage-html: test-coverage
	@echo "Generating HTML coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report saved to coverage.html"

# Run the application with example flags
run:
	./bin/ibmcloud-api-mcp --help

# Install dependencies
install:
	go mod tidy
	go mod download

# Cross-compile for multiple platforms
cross-compile: clean
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/ibmcloud-api-mcp-darwin-arm64 cmd/ibmcloud-api-mcp/main.go
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/ibmcloud-api-mcp-darwin-amd64 cmd/ibmcloud-api-mcp/main.go
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/ibmcloud-api-mcp-linux-amd64 cmd/ibmcloud-api-mcp/main.go
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/ibmcloud-api-mcp.exe cmd/ibmcloud-api-mcp/main.go

# Show help
help:
	@echo "Available targets:"
	@echo "  build             - Build the application"
	@echo "  build-optimized   - Build with size optimizations"
	@echo "  clean             - Remove built artifacts and coverage files"
	@echo "  test              - Run tests"
	@echo "  test-coverage     - Run tests with coverage report by package"
	@echo "  test-coverage-html - Generate HTML coverage report"
	@echo "  run               - Run the application with --help"
	@echo "  install           - Install/update dependencies"
	@echo "  cross-compile     - Build for multiple platforms"
	@echo "  help              - Show this help message"
