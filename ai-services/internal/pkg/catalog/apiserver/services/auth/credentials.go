package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// DefaultServiceCredentialTTL is the default lifetime of a per-deployment service credential.
	DefaultServiceCredentialTTL = 365 * 24 * time.Hour

	credentialSecretBytes = 32
	// credentialRetention is how long expired credential rows are kept before cleanup.
	credentialRetention = 24 * time.Hour
)

// ErrInvalidCredential is the single error for every failed credential exchange. It does
// not say why (malformed, unknown, expired, revoked, wrong secret, service not granted).
var ErrInvalidCredential = errors.New("invalid credential")

// credentialGrants is the fixed table of what a service credential can obtain (DA2):
// caller -> audience -> roles. It is code, not data: no request or database row can extend
// it, and it never contains RoleAdmin. Add a line only for a call one service makes to
// another.
var credentialGrants = map[string]map[string][]string{
	// chat retrieves document chunks from similarity (POST /v1/similarity-search).
	"chat": {"similarity": {RoleRead}},
}

// CanHoldCredential reports whether a service may be given a credential at all.
func CanHoldCredential(caller string) bool {
	_, ok := credentialGrants[caller]

	return ok
}

// ValidateServiceCredentialTTL rejects a non-positive credential lifetime.
func ValidateServiceCredentialTTL(ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("service credential TTL %s must be greater than 0", ttl)
	}

	return nil
}

// CredentialService creates service credentials and trades them for service tokens.
type CredentialService struct {
	tokens *TokenService
	repo   dbrepo.ServiceCredentialRepository
	ttl    time.Duration
}

// NewCredentialService creates a CredentialService. ttl is the lifetime of a new credential.
func NewCredentialService(tokens *TokenService, repo dbrepo.ServiceCredentialRepository, ttl time.Duration) (*CredentialService, error) {
	if err := ValidateServiceCredentialTTL(ttl); err != nil {
		return nil, err
	}

	return &CredentialService{tokens: tokens, repo: repo, ttl: ttl}, nil
}

// Provision creates a new credential for caller in the given deployment and returns it. This
// is the only time the secret exists in clear: the database keeps its hash. Earlier
// credentials for the same caller stay valid until they expire, so two are active while a
// deployment is rotated.
func (s *CredentialService) Provision(ctx context.Context, caller, deploymentID string) (string, error) {
	if !CanHoldCredential(caller) {
		return "", fmt.Errorf("service %q may not hold a service credential", caller)
	}

	secret, err := randomBase64URL(credentialSecretBytes)
	if err != nil {
		return "", err
	}

	id := uuid.New()
	if err := s.repo.Insert(ctx, &dbmodels.ServiceCredential{
		ID:           id,
		Caller:       caller,
		DeploymentID: deploymentID,
		SecretHash:   hashCode(secret),
		ExpiresAt:    time.Now().Add(s.ttl),
	}, credentialRetention); err != nil {
		return "", err
	}

	logger.InfofCtx(ctx, "service credential created: id=%s caller=%s deployment=%s", id, caller, deploymentID)

	return id.String() + "." + secret, nil
}

// ProvisionIfNeeded creates a credential when serviceID may hold one and returns "" when it
// may not. It lets the deployers call it for every service without a list of their own.
func (s *CredentialService) ProvisionIfNeeded(ctx context.Context, serviceID, deploymentID string) (string, error) {
	if !CanHoldCredential(serviceID) {
		return "", nil
	}

	return s.Provision(ctx, serviceID, deploymentID)
}

// Exchange trades a credential for a short-lived token for one service. The audience must
// be in credentialGrants for the credential's caller. Every failure returns
// ErrInvalidCredential, except an unexpected storage error.
func (s *CredentialService) Exchange(ctx context.Context, credential, service string) (string, time.Time, error) {
	idText, secret, ok := strings.Cut(credential, ".")
	id, err := uuid.Parse(idText)
	if !ok || err != nil || secret == "" {
		return "", time.Time{}, ErrInvalidCredential
	}

	rec, err := s.repo.GetValid(ctx, id)
	if errors.Is(err, dbrepo.ErrServiceCredentialNotFound) {
		return "", time.Time{}, ErrInvalidCredential
	}
	if err != nil {
		return "", time.Time{}, err
	}

	got := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(got[:])), []byte(rec.SecretHash)) != 1 {
		return "", time.Time{}, ErrInvalidCredential
	}

	roles, granted := credentialGrants[rec.Caller][service]
	if !granted {
		logger.WarningfCtx(ctx, "service credential %s (caller %s) asked for a token it is not granted: aud=%s", id, rec.Caller, service)

		return "", time.Time{}, ErrInvalidCredential
	}

	token, exp, err := s.tokens.issueFixed(ctx, rec.Caller, service, roles)
	if errors.Is(err, ErrUnknownService) {
		return "", time.Time{}, ErrInvalidCredential
	}

	return token, exp, err
}
