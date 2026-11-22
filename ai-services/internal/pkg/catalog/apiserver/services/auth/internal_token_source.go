package auth

import (
	"context"
	"sync"
	"time"
)

// internalTokenRefreshMargin is how long before expiry a cached token is replaced, so a
// token never expires while a request that carries it is in flight.
const internalTokenRefreshMargin = time.Minute

// InternalTokenSource hands out the catalog's own tokens (see TokenService.IssueInternalToken)
// and reuses each one until shortly before it expires. It is safe for concurrent use.
type InternalTokenSource struct {
	issuer interface {
		IssueInternalToken(ctx context.Context, service string) (string, time.Time, error)
	}
	now func() time.Time

	mu    sync.Mutex
	cache map[string]cachedToken
}

type cachedToken struct {
	token string
	exp   time.Time
}

// NewInternalTokenSource creates a source backed by the token service.
func NewInternalTokenSource(tokens *TokenService) *InternalTokenSource {
	return &InternalTokenSource{issuer: tokens, now: time.Now, cache: map[string]cachedToken{}}
}

// InternalToken returns a valid token for the service, issuing a new one when needed.
func (s *InternalTokenSource) InternalToken(ctx context.Context, service string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.cache[service]; ok && s.now().Add(internalTokenRefreshMargin).Before(c.exp) {
		return c.token, nil
	}

	token, exp, err := s.issuer.IssueInternalToken(ctx, service)
	if err != nil {
		return "", err
	}
	s.cache[service] = cachedToken{token: token, exp: exp}

	return token, nil
}

// NewInternalTokenSourceFrom creates a source backed by any issuer (tests).
func NewInternalTokenSourceFrom(issuer interface {
	IssueInternalToken(ctx context.Context, service string) (string, time.Time, error)
},
) *InternalTokenSource {
	return &InternalTokenSource{issuer: issuer, now: time.Now, cache: map[string]cachedToken{}}
}
