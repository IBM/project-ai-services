package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// AuthCodeTTL is how long an authorization code can be exchanged.
	AuthCodeTTL = 60 * time.Second
	// authCodeRetention is how long expired code rows are kept before cleanup.
	authCodeRetention = 10 * time.Minute
	authCodeBytes     = 32
	maxStateLen       = 256
	callbackPath      = "/auth/callback"
)

var (
	// ErrUnknownService is returned for a service name that is not in the registry.
	ErrUnknownService = errors.New("unknown service")
	// ErrForbidden is returned when the resolver grants the caller no role on the service.
	ErrForbidden = errors.New("no access to service")
	// ErrInvalidRedirect is returned when a redirect_uri is not an exact registered callback.
	ErrInvalidRedirect = errors.New("redirect_uri is not registered")
	// ErrInvalidRequest is returned for a malformed authorize or exchange request.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrInvalidGrant is the single error for every failed code exchange. It deliberately
	// does not say why (unknown, used, expired, wrong verifier, wrong redirect).
	ErrInvalidGrant = errors.New("invalid grant")

	challengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	verifierRe  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// TokenService issues service tokens, directly (CLI) or through the authorization-code flow (browser).
type TokenService struct {
	users    repository.UserRepository
	resolver ScopeResolver
	registry *ServiceRegistry
	signer   *Signer
	codes    dbrepo.AuthCodeRepository
	targets  dbrepo.RedirectTargetRepository
	ttl      time.Duration
}

// NewTokenService creates a TokenService. ttl must be positive and not above MaxServiceTokenTTL.
func NewTokenService(
	users repository.UserRepository,
	resolver ScopeResolver,
	registry *ServiceRegistry,
	signer *Signer,
	codes dbrepo.AuthCodeRepository,
	targets dbrepo.RedirectTargetRepository,
	ttl time.Duration,
) (*TokenService, error) {
	if err := ValidateServiceTokenTTL(ttl); err != nil {
		return nil, err
	}

	return &TokenService{users: users, resolver: resolver, registry: registry, signer: signer, codes: codes, targets: targets, ttl: ttl}, nil
}

// ValidateServiceTokenTTL rejects a non-positive TTL or one above the ceiling.
func ValidateServiceTokenTTL(ttl time.Duration) error {
	if ttl <= 0 || ttl > MaxServiceTokenTTL {
		return fmt.Errorf("service token TTL %s must be greater than 0 and at most %s", ttl, MaxServiceTokenTTL)
	}

	return nil
}

// IssueServiceToken issues a token for an authenticated catalog user. The caller must
// already have authenticated userID (access-token middleware).
func (s *TokenService) IssueServiceToken(ctx context.Context, userID, service string) (string, time.Time, error) {
	if _, ok := s.registry.Lookup(service); !ok {
		return "", time.Time{}, ErrUnknownService
	}

	return s.issue(ctx, userID, service)
}

// internalGrants is the fixed list of roles the catalog may give itself on a service (DA2).
// It is deliberately not part of the ScopeResolver: no request, user or database row can
// extend it, and it never contains RoleAdmin. Add a line only for a call the catalog
// itself makes to that service.
var internalGrants = map[string][]string{
	// The catalog manages connectors on Digitize (POST/PUT/DELETE/GET /v1/connectors).
	"digitize": {RoleWrite},
}

// IssueInternalToken issues a token for a call the catalog makes itself, with typ=service
// and sub=InternalSubject. The roles come only from internalGrants, so a service that is
// not listed gets ErrUnknownService, and RoleAdmin can never be granted this way.
func (s *TokenService) IssueInternalToken(ctx context.Context, service string) (string, time.Time, error) {
	roles, ok := internalGrants[service]
	if !ok {
		return "", time.Time{}, ErrUnknownService
	}

	return s.issueFixed(ctx, InternalSubject, service, roles)
}

// issueFixed signs a typ=service, idp=internal token for subject with exactly these roles.
// The caller must take roles from a fixed in-code table, never from a request.
func (s *TokenService) issueFixed(ctx context.Context, subject, service string, roles []string) (string, time.Time, error) {
	if _, ok := s.registry.Lookup(service); !ok {
		return "", time.Time{}, ErrUnknownService
	}

	scopes := make([]string, len(roles))
	for i, r := range roles {
		scopes[i] = service + ":" + r
	}

	jti, err := randomHex(jtiBytes)
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	exp := now.Add(s.ttl)
	token, err := s.signer.Sign(ServiceClaims{
		Scope: strings.Join(scopes, " "),
		IDP:   IDPInternal,
		Type:  jwtTypeService,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ServiceTokenIssuer,
			Subject:   subject,
			Audience:  []string{service},
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        jti,
		},
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign internal token: %w", err)
	}

	logger.DebugfCtx(ctx, "internal token issued: jti=%s sub=%s aud=%s scope=%q", jti, subject, service, strings.Join(scopes, " "))

	return token, exp, nil
}

func (s *TokenService) issue(ctx context.Context, userID, service string) (string, time.Time, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: get user: %w", ErrForbidden, err)
	}

	p := Principal{ID: user.ID, IDP: user.IDP, Type: jwtTypeUser}
	roles, err := s.resolver.Resolve(ctx, p, service)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("resolve roles: %w", err)
	}
	if len(roles) == 0 {
		return "", time.Time{}, ErrForbidden
	}

	scopes := make([]string, len(roles))
	for i, r := range roles {
		scopes[i] = service + ":" + r
	}
	scopeStr := strings.Join(scopes, " ")

	jti, err := randomHex(jtiBytes)
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now()
	exp := now.Add(s.ttl)
	token, err := s.signer.Sign(ServiceClaims{
		Scope: scopeStr,
		IDP:   p.IDP,
		Type:  p.Type,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ServiceTokenIssuer,
			Subject:   p.ID,
			Audience:  []string{service},
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        jti,
		},
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign service token: %w", err)
	}

	logger.InfofCtx(ctx, "service token issued: jti=%s sub=%s aud=%s scope=%q", jti, p.ID, service, scopeStr)

	return token, exp, nil
}

// AuthorizeRequest is the body of POST /auth/authorize.
type AuthorizeRequest struct {
	Service       string
	RedirectURI   string
	CodeChallenge string
	State         string
}

// Authorize validates the request for an authenticated user and returns the URL to send
// the browser to, carrying a one-time code and the caller's state. The URL is built from
// the registered redirect URI, never from client-supplied pieces other than state.
func (s *TokenService) Authorize(ctx context.Context, userID string, req AuthorizeRequest) (string, error) {
	if _, ok := s.registry.Lookup(req.Service); !ok {
		return "", ErrUnknownService
	}
	if !challengeRe.MatchString(req.CodeChallenge) || req.State == "" || len(req.State) > maxStateLen {
		return "", ErrInvalidRequest
	}
	registered, err := s.matchRedirect(ctx, req.Service, req.RedirectURI)
	if err != nil {
		return "", err
	}

	user, err := s.requireGrant(ctx, userID, req.Service)
	if err != nil {
		return "", err
	}

	code, err := randomBase64URL(authCodeBytes)
	if err != nil {
		return "", err
	}
	if err := s.codes.Insert(ctx, &dbmodels.AuthCode{
		CodeHash:      hashCode(code),
		UserID:        user.ID,
		IDP:           user.IDP,
		Service:       req.Service,
		RedirectURI:   registered,
		CodeChallenge: req.CodeChallenge,
		ExpiresAt:     time.Now().Add(AuthCodeTTL),
	}, authCodeRetention); err != nil {
		return "", err
	}

	q := url.Values{}
	q.Set("code", code)
	q.Set("state", req.State)

	return registered + "?" + q.Encode(), nil
}

// requireGrant loads the user and returns ErrForbidden unless the resolver grants at
// least one role for the service.
func (s *TokenService) requireGrant(ctx context.Context, userID, service string) (*models.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrForbidden
	}
	roles, err := s.resolver.Resolve(ctx, Principal{ID: user.ID, IDP: user.IDP, Type: jwtTypeUser}, service)
	if err != nil {
		return nil, fmt.Errorf("resolve roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, ErrForbidden
	}

	return user, nil
}

// ExchangeRequest is the body of POST /auth/code/exchange.
type ExchangeRequest struct {
	Service      string
	RedirectURI  string
	Code         string
	CodeVerifier string
}

// Exchange trades a one-time code and its PKCE verifier for a service token. Every
// failure returns ErrInvalidGrant (or an internal error), so a caller cannot tell which
// check failed. The code is consumed first: a wrong verifier burns it.
func (s *TokenService) Exchange(ctx context.Context, req ExchangeRequest) (string, time.Time, error) {
	if req.Code == "" || !verifierRe.MatchString(req.CodeVerifier) {
		return "", time.Time{}, ErrInvalidGrant
	}

	hash := hashCode(req.Code)
	rec, err := s.codes.Consume(ctx, hash)
	if errors.Is(err, dbrepo.ErrAuthCodeNotFound) {
		if used, uerr := s.codes.WasUsed(ctx, hash); uerr == nil && used {
			logger.WarningfCtx(ctx, "authorization code presented again after use (possible interception or client retry)")
		}

		return "", time.Time{}, ErrInvalidGrant
	}
	if err != nil {
		return "", time.Time{}, err
	}

	if !grantMatches(rec, req) {
		return "", time.Time{}, ErrInvalidGrant
	}

	// The resolver runs again, so a grant removed after authorize yields no token.
	token, exp, err := s.issue(ctx, rec.UserID, rec.Service)
	if errors.Is(err, ErrForbidden) {
		return "", time.Time{}, ErrInvalidGrant
	}

	return token, exp, err
}

// grantMatches checks the PKCE verifier, service and redirect URI against the stored code.
func grantMatches(rec *dbmodels.AuthCode, req ExchangeRequest) bool {
	sum := sha256.Sum256([]byte(req.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	return subtle.ConstantTimeCompare([]byte(challenge), []byte(rec.CodeChallenge)) == 1 &&
		rec.Service == req.Service &&
		rec.RedirectURI == req.RedirectURI
}

// matchRedirect returns the registered callback equal to uri, or ErrInvalidRedirect.
// A callback is the `ui` endpoint of a deployed instance of the service plus /auth/callback,
// compared byte for byte. No wildcard, prefix, query, fragment or userinfo is accepted.
func (s *TokenService) matchRedirect(ctx context.Context, service, uri string) (string, error) {
	if !wellFormedCallback(uri) {
		return "", ErrInvalidRedirect
	}

	def, _ := s.registry.Lookup(service)
	uis, err := s.targets.UIEndpoints(ctx, def.CatalogID)
	if err != nil {
		return "", fmt.Errorf("load redirect targets: %w", err)
	}

	for _, ui := range uis {
		if strings.TrimRight(ui, "/")+callbackPath == uri {
			return uri, nil
		}
	}

	return "", ErrInvalidRedirect
}

// wellFormedCallback accepts https, or http only for a loopback host, with no userinfo,
// query or fragment.
func wellFormedCallback(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}

	return u.Scheme == "https" || (u.Scheme == "http" && isLoopbackHost(u.Hostname()))
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))

	return hex.EncodeToString(sum[:])
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}
