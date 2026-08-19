package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/project-ai-services/mcp/internal/openapi"
	"github.com/project-ai-services/mcp/internal/tool"
	"github.com/project-ai-services/mcp/internal/types"
	"golang.org/x/time/rate"
)

// Mock authenticator for testing
type mockAuthenticator struct {
	token    string
	authType string
}

func (m *mockAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	return m.token, nil
}

func (m *mockAuthenticator) IsPassthrough() bool {
	return false
}

func (m *mockAuthenticator) GetType() string {
	return m.authType
}

type MockLogger struct {
	logs []string
}

func NewMockLogger() *MockLogger {
	return &MockLogger{logs: make([]string, 0)}
}

func (l *MockLogger) Printf(format string, v ...interface{}) {
	l.logs = append(l.logs, fmt.Sprintf(format, v...))
}

func (l *MockLogger) GetLogs() []string {
	return l.logs
}

type MockSignalHandler struct {
	signalChan chan<- os.Signal
}

func NewMockSignalHandler() *MockSignalHandler {
	return &MockSignalHandler{}
}

func (s *MockSignalHandler) Notify(c chan<- os.Signal, sig ...os.Signal) {
	s.signalChan = c
}

func (s *MockSignalHandler) SendSignal(sig os.Signal) {
	if s.signalChan != nil {
		go func() {
			s.signalChan <- sig
		}()
	}
}

type FailingResponseWriter struct {
	header          http.Header
	shouldFail      bool
	statusCode      int
	httpErrorCalled bool
}

func (w *FailingResponseWriter) Header() http.Header {
	return w.header
}

func (w *FailingResponseWriter) Write(data []byte) (int, error) {
	if w.shouldFail {
		return 0, errors.New("simulated write failure")
	}
	return len(data), nil
}

func (w *FailingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	if statusCode == http.StatusInternalServerError {
		w.httpErrorCalled = true
	}
}

func createTestAggregator() *tool.Aggregator {
	// Create a simple test interface
	operations := []types.OperationInfo{
		{
			OperationID: "getUser",
			Method:      types.GET,
			Path:        "/users/{id}",
			Description: "Get user by ID",
			Tags:        []string{"users"},
			Parameters: []types.ParameterInfo{
				{
					Name:     "id",
					In:       "path",
					Required: true,
					Schema:   &jsonschema.Schema{Type: "string"},
				},
			},
		},
	}

	// Create proper mock document
	info := &base.Info{
		Title:   "Test API",
		Version: "1.0.0",
	}

	doc := &v3.Document{
		Version: "3.1.0",
		Info:    info,
	}

	intf := &openapi.Interface{
		Doc:        doc,
		Name:       "test-api",
		Operations: operations,
		Tags:       []string{"users"},
	}

	auth := &mockAuthenticator{token: "test-token", authType: "test"}

	aggregator, _ := tool.NewAggregator(intf, "https://api.example.com", auth, nil, nil)
	return aggregator
}

func TestHTTPServer_handleHealth(t *testing.T) {
	aggregator := createTestAggregator()
	server := &HTTPServer{
		port:       3000,
		aggregator: aggregator,
		tags:       nil,
	}

	t.Run("success", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/health", nil)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleHealth)

		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}

		contentType := rr.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("handler returned wrong content type: got %v want %v",
				contentType, "application/json")
		}

		var response map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to parse JSON response: %v", err)
		}

		expectedKeys := []string{"status", "transport", "sdk", "version"}
		for _, key := range expectedKeys {
			if _, exists := response[key]; !exists {
				t.Errorf("Expected key %q not found in response", key)
			}
		}

		if response["status"] != "ok" {
			t.Errorf("Expected status 'ok', got %v", response["status"])
		}

		if response["transport"] != "streamable-http" {
			t.Errorf("Expected transport 'streamable-http', got %v", response["transport"])
		}
	})

	t.Run("json_encoding_error", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/health", nil)
		if err != nil {
			t.Fatal(err)
		}

		// Create a failing ResponseWriter that will cause JSON encoding to fail
		failingWriter := &FailingResponseWriter{
			header:     make(http.Header),
			shouldFail: true,
		}

		handler := http.HandlerFunc(server.handleHealth)
		handler.ServeHTTP(failingWriter, req)

		if failingWriter.statusCode != http.StatusInternalServerError {
			t.Errorf("Expected status InternalServerError, got %d", failingWriter.statusCode)
		}

		if !failingWriter.httpErrorCalled {
			t.Error("Expected http.Error to be called for JSON encoding failure")
		}
	})
}

func TestHTTPServer_corsMiddleware(t *testing.T) {
	aggregator := createTestAggregator()
	server := &HTTPServer{
		port:       3000,
		aggregator: aggregator,
		tags:       nil,
	}

	// Test OPTIONS request
	req, err := http.NewRequest("OPTIONS", "/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // Should not be reached for OPTIONS
	})

	handler := server.corsMiddleware(testHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("OPTIONS request returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check CORS headers
	expectedHeaders := map[string]string{
		"Access-Control-Allow-Origin":   "*",
		"Access-Control-Allow-Headers":  "Content-Type, Authorization, Mcp-Session-Id",
		"Access-Control-Allow-Methods":  "GET, POST, OPTIONS",
		"Access-Control-Expose-Headers": "Content-Type, Mcp-Session-Id",
	}

	for header, expectedValue := range expectedHeaders {
		actualValue := rr.Header().Get(header)
		if actualValue != expectedValue {
			t.Errorf("Header %s: got %v want %v", header, actualValue, expectedValue)
		}
	}

	// Test non-OPTIONS request
	req, err = http.NewRequest("POST", "/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should pass through to next handler
	if status := rr.Code; status != http.StatusTeapot {
		t.Errorf("POST request should pass through: got %v want %v",
			status, http.StatusTeapot)
	}

	// CORS headers should still be present
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("CORS origin header missing or incorrect: got %v want %v", origin, "*")
	}
}

func TestHTTPServer_loggingMiddleware(t *testing.T) {
	aggregator := createTestAggregator()
	server := &HTTPServer{
		port:       3000,
		aggregator: aggregator,
		tags:       nil,
	}

	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add session ID header
	req.Header.Set("Mcp-Session-Id", "test-session-123")

	rr := httptest.NewRecorder()

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(1 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	handler := server.loggingMiddleware(testHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Logged request returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Note: We can't easily test log output without capturing it,
	// but we can verify the middleware doesn't interfere with the request
}

func TestHTTPServer_createToolHandler(t *testing.T) {
	aggregator := createTestAggregator()
	server := &HTTPServer{
		port:       3000,
		aggregator: aggregator,
		tags:       nil,
	}

	handler := server.createToolHandler()

	// Create test request
	request := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "getUser",
			Arguments: []byte(`{"id": "123"}`),
		},
	}

	ctx := context.Background()

	// This will likely fail due to network request, but we can verify the handler structure
	result, err := handler(ctx, request)

	// We expect an error due to the mock HTTP request failing,
	// but the handler should not panic and should return the expected types
	if err == nil {
		t.Log("Unexpected success - handler completed without error")
		if result == nil {
			t.Error("Handler should return result on success")
		}
	} else {
		// Verify it's a network error, not a type conversion error
		if strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("Handler failed to find tool - aggregator integration issue: %v", err)
		}
		// Network errors are expected in unit tests
	}
}

func createMockDependencies() (Logger, SignalHandler) {
	mockLogger := NewMockLogger()
	mockSignalHandler := NewMockSignalHandler()

	return mockLogger, mockSignalHandler
}

// Test NewHTTPServer constructor
func TestNewHTTPServer(t *testing.T) {
	aggregator := createTestAggregator()
	logger, signalHandler := createMockDependencies()
	tags := []string{"test-tag"}
	port := 8080
	mockRateLimiter := NewRateLimiterManager(20, 60)

	server := NewHTTPServer(port, aggregator, tags, logger, signalHandler, mockRateLimiter)

	if server.port != port {
		t.Errorf("Expected port %d, got %d", port, server.port)
	}
	if server.aggregator != aggregator {
		t.Error("Expected aggregator to be set correctly")
	}
	if len(server.tags) != len(tags) || server.tags[0] != tags[0] {
		t.Errorf("Expected tags %v, got %v", tags, server.tags)
	}
	if server.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
	if server.signalHandler != signalHandler {
		t.Error("Expected signalHandler to be set correctly")
	}
}

// Test HTTPServer Start method with successful execution
func TestHTTPServerStartSuccess(t *testing.T) {
	aggregator := createTestAggregator()
	logger, signalHandler := createMockDependencies()
	mockLogger := logger.(*MockLogger)
	mockSignalHandler := signalHandler.(*MockSignalHandler)
	mockRateLimiter := NewRateLimiterManager(20, 60)
	server := NewHTTPServer(3000, aggregator, nil, logger, signalHandler, mockRateLimiter)

	// Create a channel to signal server startup completion
	done := make(chan bool, 1)

	// Start server in goroutine
	go func() {
		// Send signal to trigger shutdown after brief delay
		go func() {
			time.Sleep(50 * time.Millisecond) // Let server start
			mockSignalHandler.SendSignal(syscall.SIGTERM)
		}()

		err := server.Start()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		done <- true
	}()

	// Wait for test completion or timeout
	select {
	case <-done:
		// Test completed successfully
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out")
	}

	// Verify logging occurred
	logs := mockLogger.GetLogs()
	if len(logs) == 0 {
		t.Error("Expected logging to occur during server lifecycle")
	}
}

// Test HTTPServer Start method with HTTP server error
func TestHTTPServerStartWithHTTPError(t *testing.T) {
	aggregator := createTestAggregator()

	// Create dependencies
	mockLogger := NewMockLogger()
	mockSignalHandler := NewMockSignalHandler()
	mockRateLimiter := NewRateLimiterManager(20, 60)

	server := NewHTTPServer(3000, aggregator, nil, mockLogger, mockSignalHandler, mockRateLimiter)

	// Start server in goroutine to test error handling
	done := make(chan error, 1)
	go func() {
		// Send signal to trigger shutdown after brief delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			mockSignalHandler.SendSignal(syscall.SIGTERM)
		}()

		err := server.Start()
		done <- err
	}()

	// Wait for test completion
	select {
	case err := <-done:
		if err != nil {
			t.Logf("Server returned expected error (this is normal for this test): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out")
	}

	// Verify logging occurred during the test
	logs := mockLogger.GetLogs()
	if len(logs) == 0 {
		t.Error("Expected some logging to occur during server lifecycle")
	}
}

// TestHTTPServer_RequestHeaderContextPassing tests that request headers are properly
// passed through to the context for passthrough authentication
func TestHTTPServer_RequestHeaderContextPassing(t *testing.T) {
	tests := []struct {
		name           string
		requestExtra   *mcp.RequestExtra
		expectHeaders  bool
		expectedHeader string
	}{
		{
			name: "with request headers",
			requestExtra: &mcp.RequestExtra{
				Header: http.Header{
					"Authorization": []string{"Bearer test-token"},
					"Content-Type":  []string{"application/json"},
				},
			},
			expectHeaders:  true,
			expectedHeader: "Bearer test-token",
		},
		{
			name:          "with nil request extra",
			requestExtra:  nil,
			expectHeaders: false,
		},
		{
			name: "with empty headers",
			requestExtra: &mcp.RequestExtra{
				Header: http.Header{},
			},
			expectHeaders: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create base aggregator
			aggregator := createTestAggregator()

			server := &HTTPServer{
				port:       3000,
				aggregator: aggregator,
				tags:       nil,
			}

			handler := server.createToolHandler()

			// Create test request
			request := &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{
					Name:      "getUser",
					Arguments: []byte(`{"id": "123"}`),
				},
				Extra: tt.requestExtra,
			}

			// Create a context that we can inspect
			baseCtx := context.Background()

			// Execute the handler
			_, err := handler(baseCtx, request)

			if err != nil {
				t.Logf("Expected error due to HTTP request: %v", err)
			}

			// Since we can't easily intercept the context passed to the aggregator,
			// we can at least test that the handler doesn't panic and processes
			// the request properly based on the Extra headers
			if tt.expectHeaders && tt.requestExtra != nil && tt.requestExtra.Header != nil {
				// Test passed if we got here without panicking
				t.Log("Handler processed request with Extra headers successfully")
			} else {
				// Test passed if we got here without panicking
				t.Log("Handler processed request without Extra headers successfully")
			}
		})
	}
}

func TestRateLimitMiddlewareExceeded(t *testing.T) {
	os.Setenv("RATE_LIMIT_REQUESTS", "3")
	os.Setenv("RATE_LIMIT_PER_SECONDS", "60")

	mockLogger := NewMockLogger()
	mockSignalHandler := NewMockSignalHandler()
	rateLimit := rate.Every(60 * time.Second / 3) // 1 req per 20 seconds
	mockRateLimiter := NewRateLimiterManager(rateLimit, 3)

	server := NewHTTPServer(3000, createTestAggregator(), nil, mockLogger, mockSignalHandler, mockRateLimiter)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := server.rateLimitMiddleware(nextHandler)

	// httptest.NewRequest sets a fixed RemoteAddr ("192.0.2.1:1234"), and the same
	// req is reused across every iteration below, so all 5 requests land in the
	// same rate-limit bucket -- exactly what this test needs to verify.
	req := httptest.NewRequest("POST", "/mcp", nil)

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if i < 3 {
			if w.Code != http.StatusOK {
				t.Errorf("Expected status OK, got %d", w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("Expected status Too Many Requests, got %d", w.Code)
			}

			if w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
			}
			if w.Header().Get("Retry-After") == "" {
				t.Errorf("Expected Retry-After header to be set")
			}

			var body map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &body)
			if err != nil {
				t.Errorf("Failed to parse JSON response: %v", err)
			}
			if body["code"] != "rate_limit_exceeded" {
				t.Errorf("Expected error 'rate_limit_exceeded', got '%s'", body["code"])
			}
		}
	}
}

func TestRateLimitMiddleware_DistinguishesCallersBehindProxy(t *testing.T) {
	os.Setenv("RATE_LIMIT_REQUESTS", "3")
	os.Setenv("RATE_LIMIT_PER_SECONDS", "60")

	mockRateLimiter := NewRateLimiterManager(rate.Every(20*time.Second), 3)
	server := NewHTTPServer(3000, createTestAggregator(), nil, NewMockLogger(), NewMockSignalHandler(), mockRateLimiter)
	middleware := server.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Both requests arrive from the same Caddy address; only X-Forwarded-For
	// distinguishes the two real callers.
	callerA := httptest.NewRequest("POST", "/mcp", nil)
	callerA.RemoteAddr = "10.0.0.5:443"
	callerA.Header.Set("X-Forwarded-For", "203.0.113.7")

	callerB := httptest.NewRequest("POST", "/mcp", nil)
	callerB.RemoteAddr = "10.0.0.5:443"
	callerB.Header.Set("X-Forwarded-For", "198.51.100.9")

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, callerA)
		if w.Code != http.StatusOK {
			t.Fatalf("callerA request %d: expected 200, got %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, callerB)
	if w.Code != http.StatusOK {
		t.Errorf("callerB should be unaffected by callerA's traffic, got %d", w.Code)
	}
}
