package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/project-ai-services/mcp/internal/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"
)

type TokenValidator interface {
	GetClaims(token string, validateExp bool) (interface{}, error)
}

type Logger interface {
	Printf(format string, v ...interface{})
}

type SignalHandler interface {
	Notify(c chan<- os.Signal, sig ...os.Signal)
}

type RateLimiter interface {
	GetLimiter(iamID string) *rate.Limiter
}

type RateLimiterManager struct {
	limiters map[string]*rate.Limiter
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
}

func NewRateLimiterManager(limit rate.Limit, burst int) *RateLimiterManager {
	return &RateLimiterManager{
		limiters: make(map[string]*rate.Limiter),
		limit:    limit,
		burst:    burst,
	}
}

func (r *RateLimiterManager) GetLimiter(iamID string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	limiter, exists := r.limiters[iamID]
	if !exists {
		limiter = rate.NewLimiter(r.limit, r.burst)
		r.limiters[iamID] = limiter
	}
	return limiter
}

func GetRateLimiterConfig() (rate.Limit, int, error) {
	rateVal := float64(20) // set default to 20
	var err error
	if rateValStr, ok := os.LookupEnv("RATE_LIMIT_REQUESTS"); ok {
		rateVal, err = strconv.ParseFloat(rateValStr, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid RATE_LIMIT_REQUESTS: %w", err)
		}
	}

	windowVal := float64(60) // set default to 60
	if windowValStr, ok := os.LookupEnv("RATE_LIMIT_PER_SECONDS"); ok {
		windowVal, err = strconv.ParseFloat(windowValStr, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid RATE_LIMIT_PER_SECONDS: %w", err)
		}
	}

	if windowVal <= 0 {
		return 0, 0, fmt.Errorf("RATE_LIMIT_PER_SECONDS must be greater than zero")
	}

	return rate.Limit(rateVal / windowVal), int(rateVal), nil
}

type TokenValidatorAdapter struct {
	validator *JWTTokenValidator
}

func NewTokenValidatorAdapter(validator *JWTTokenValidator) *TokenValidatorAdapter {
	return &TokenValidatorAdapter{
		validator: validator,
	}
}

func (t *TokenValidatorAdapter) GetClaims(tokenStr string, validateExp bool) (interface{}, error) {
	return t.validator.GetClaims(tokenStr, validateExp)
}

type StdLogger struct{}

func (l *StdLogger) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

type OSSignalHandler struct{}

func (o *OSSignalHandler) Notify(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}

type HTTPServer struct {
	port           int
	aggregator     *tool.Aggregator
	tags           []string
	tokenValidator TokenValidator
	logger         Logger
	signalHandler  SignalHandler
	rateLimiter    RateLimiter
}

func NewHTTPServer(
	port int,
	aggregator *tool.Aggregator,
	tags []string,
	tokenValidator TokenValidator,
	logger Logger,
	signalHandler SignalHandler,
	rateLimiter RateLimiter,
) *HTTPServer {
	return &HTTPServer{
		port:           port,
		aggregator:     aggregator,
		tags:           tags,
		tokenValidator: tokenValidator,
		logger:         logger,
		signalHandler:  signalHandler,
		rateLimiter:    rateLimiter,
	}
}

func (s *HTTPServer) Start() error {
	impl := &mcp.Implementation{
		Name:    s.aggregator.GetFriendlyName(),
		Version: "1.0.0",
	}

	mcpServer := mcp.NewServer(impl, &mcp.ServerOptions{})

	tools := s.aggregator.GetTools(s.tags)

	handler := s.createToolHandler()

	for _, tool := range tools {
		mcpServer.AddTool(tool, handler)
	}

	streamHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{})

	mux := http.NewServeMux()

	combinedHandler := s.tokenValidationMiddleware(
		s.rateLimitMiddleware(
			s.corsMiddleware(streamHandler),
		),
		s.tokenValidator,
	)

	mux.Handle("/mcp", combinedHandler)
	mux.HandleFunc("/health", s.handleHealth)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           s.loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	s.signalHandler.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		s.logger.Printf("MCP Streamable HTTP Server listening on port %d\n", s.port)
		s.logger.Printf("Health check available at http://localhost:%d/health\n", s.port)
		s.logger.Printf("MCP endpoint available at http://localhost:%d/mcp\n", s.port)
		s.logger.Printf("Mode: Streamable HTTP\n")

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	<-sigChan
	s.logger.Printf("Shutting down HTTP server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Printf("HTTP server shutdown error: %v", err)
		return err
	}

	s.logger.Printf("HTTP server shutdown complete")
	return nil
}

func (s *HTTPServer) createToolHandler() mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {

		if request.Extra != nil && request.Extra.Header != nil {
			ctx = context.WithValue(ctx, "requestHeaders", request.Extra.Header)
		}

		genericParams := &mcp.CallToolParamsRaw{
			Meta:      request.Params.Meta,
			Name:      request.Params.Name,
			Arguments: request.Params.Arguments,
		}

		result, err := s.aggregator.HandleToolCall(ctx, genericParams)
		if err != nil {
			return nil, err
		}

		return result, nil
	}
}

type contextKey string

const claimsContextKey contextKey = "claims"

func (s *HTTPServer) tokenValidationMiddleware(next http.Handler, validator TokenValidator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		if tok == "" {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		claims, err := validator.GetClaims(strings.TrimSpace(strings.TrimPrefix(tok, "Bearer")), false)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *HTTPServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type, Mcp-Session-Id")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sessionID := r.Header.Get("Mcp-Session-Id")
		next.ServeHTTP(w, r)
		log.Printf("%s %s %v session=%s", r.Method, r.URL.Path, time.Since(start), sessionID)
	})
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	sdkVersion := "unknown"
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range buildInfo.Deps {
			if dep.Path == "github.com/modelcontextprotocol/go-sdk" {
				sdkVersion = dep.Version
				break
			}
		}
	}

	response := map[string]any{
		"status":    "ok",
		"transport": "streamable-http",
		"sdk":       "mcp-go-sdk",
		"version":   sdkVersion,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding health response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Add("Accept", "text/event-stream")

		claims, ok := r.Context().Value(claimsContextKey).(*IAMAccessTokenClaims)
		if !ok || claims.IAMID == "" {
			http.Error(w, "Invalid or missing claims", http.StatusUnauthorized)
			return
		}

		IAMID := claims.IAMID
		limiter := s.rateLimiter.GetLimiter(IAMID)

		// another place the default is used... also why is it a string here -> fix default
		retryAfter, err := strconv.Atoi(os.Getenv("RATE_LIMIT_PER_SECONDS"))
		if err != nil || retryAfter <= 0 {
			retryAfter = 60
		}
		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Accept", "text/event-stream")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			w.WriteHeader(http.StatusTooManyRequests)
			if err := json.NewEncoder(w).Encode(map[string]string{
				"code":        "rate_limit_exceeded",
				"message":     "You have made too many requests. Please wait and try again soon.",
				"retry_after": fmt.Sprintf("%d", retryAfter),
			}); err != nil {
				log.Printf("Error encoding rate limit response: %v", err)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
