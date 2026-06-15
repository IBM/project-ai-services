package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// CtxRequestIDKey is the key used to store the request ID in the Gin context
	CtxRequestIDKey = "request_id"
	// HeaderRequestID is the HTTP header name for the request ID
	HeaderRequestID = "X-Request-ID"
)

// RequestIDMiddleware is a Gin middleware that generates or extracts a unique request ID
// for each incoming request. If a request ID is provided in the X-Request-ID header,
// it will be used; otherwise, a new UUID will be generated. The request ID is then:
// - Set in the Gin context for use by downstream handlers
// - Added to the response headers
// - Set in the logger context so all logs include the request ID (when AI_SERVICES_LOG_FORMAT=service)
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request ID is already provided in the header
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			// Generate a new UUID if not provided
			requestID = uuid.New().String()
		}

		// Store request ID in Gin context for downstream handlers
		c.Set(CtxRequestIDKey, requestID)

		// Add request ID to response headers
		c.Header(HeaderRequestID, requestID)

		// Create a context with the request ID and set it in the logger
		ctx := context.WithValue(context.Background(), logger.RequestIDKey, requestID)
		logger.SetRequestID(ctx)

		// Log the incoming request
		logger.Infof("Incoming request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Continue processing the request
		c.Next()

		// Log the response status
		logger.Infof("Request completed: %s %s - Status: %d", c.Request.Method, c.Request.URL.Path, c.Writer.Status())

		// Clear the request ID from logger after request is complete
		logger.ClearRequestID()
	}
}

// Made with Bob
