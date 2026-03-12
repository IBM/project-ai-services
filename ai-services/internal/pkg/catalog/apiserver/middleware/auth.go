package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
)

const (
	CtxUserIDKey   = "user_id"
	CtxRawTokenKey = "raw_token"
)

// AuthMiddleware is a Gin middleware function that validates JWT access tokens for protected routes.
// It checks for the presence of a Bearer token in the Authorization header, validates it using the
// provided TokenManager, and checks against the blacklist to ensure the token has not been revoked.
// If the token is valid, it extracts the user ID and token expiry time, sets them in the Gin context
// for downstream handlers, and allows the request to proceed. If any validation step fails, it aborts
// the request with a 401 Unauthorized response and an appropriate error message.
func AuthMiddleware(tokenMgr *auth.TokenManager, blacklist repository.TokenBlacklist) gin.HandlerFunc {
	return func(c *gin.Context) {
		ah := c.GetHeader("Authorization")
		if ah == "" || !strings.HasPrefix(ah, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})

			return
		}
		raw := strings.TrimPrefix(ah, "Bearer ")
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})

			return
		}
		if blacklist.Contains(raw) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token revoked"})

			return
		}

		uid, exp, err := tokenMgr.ValidateAccessToken(raw)
		if err != nil || uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})

			return
		}

		// Propagate context
		c.Set(CtxUserIDKey, uid)
		c.Set(CtxRawTokenKey, raw)
		c.Header("X-Token-Exp", exp.UTC().Format("2006-01-02T15:04:05Z"))
		c.Next()
	}
}
