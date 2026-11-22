package common

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// Services the catalog issues tokens for. The service name is the token audience, so a request
// must carry the token of the service it is sent to.
const (
	ServiceChat       = "chat"
	ServiceDigitize   = "digitize"
	ServiceSimilarity = "similarity"
	ServiceSummarize  = "summarize"
)

// ServiceTokenSource issues a service token for one service on behalf of the user who ran
// `catalog login`. It is a variable so tests can replace it.
var ServiceTokenSource = catalogClient.ServiceTokenForLoggedInUser

const serviceTokenRefreshSkew = 30 * time.Second

type cachedServiceToken struct {
	token string
	exp   time.Time
}

var (
	serviceTokenMu    sync.Mutex
	serviceTokenCache = map[string]cachedServiceToken{}
)

// ServiceToken returns a catalog-issued token whose audience is service. The services reject the
// catalog access token. A token is reused until 30 seconds before it expires.
func ServiceToken(ctx context.Context, service string) (string, error) {
	serviceTokenMu.Lock()
	defer serviceTokenMu.Unlock()

	if c, ok := serviceTokenCache[service]; ok && time.Until(c.exp) > serviceTokenRefreshSkew {
		return c.token, nil
	}

	token, err := ServiceTokenSource(ctx, service)
	if err != nil {
		return "", fmt.Errorf("could not get a %s service token: %w", service, err)
	}

	// A token whose lifetime is unknown is returned but not cached.
	if exp, expErr := jwtExpiry(token); expErr == nil {
		serviceTokenCache[service] = cachedServiceToken{token: token, exp: exp}
	}

	return token, nil
}

// ResetServiceTokenCache drops every cached token. Tests use it.
func ResetServiceTokenCache() {
	serviceTokenMu.Lock()
	defer serviceTokenMu.Unlock()

	serviceTokenCache = map[string]cachedServiceToken{}
}

// jwtExpiry decodes the exp claim from a JWT without verifying the signature.
func jwtExpiry(token string) (time.Time, error) {
	const jwtPartCount = 3

	parts := strings.Split(token, ".")
	if len(parts) != jwtPartCount {
		return time.Time{}, fmt.Errorf("malformed JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp claim")
	}

	return time.Unix(claims.Exp, 0), nil
}
