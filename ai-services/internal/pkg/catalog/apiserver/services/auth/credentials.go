package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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

// ValidateGrants rejects grants a credential must never hold: none at all, an audience that
// is not a registered service, an empty role list, or RoleAdmin.
func (s *CredentialService) ValidateGrants(grants map[string][]string) error {
	if len(grants) == 0 {
		return errors.New("a service credential needs at least one grant")
	}
	for aud, roles := range grants {
		if _, ok := s.tokens.registry.Lookup(aud); !ok {
			return fmt.Errorf("grant audience %q is not a known service", aud)
		}
		if len(roles) == 0 {
			return fmt.Errorf("grant for %q has no roles", aud)
		}
		for _, r := range roles {
			if r == RoleAdmin {
				return fmt.Errorf("grant for %q may not include %q", aud, RoleAdmin)
			}
		}
	}

	return nil
}

// ProvisionWithGrants creates a new credential for caller in the given deployment and
// returns it. The grants come from the service's asset (its @credential annotation). They are
// stored with the credential and are what Exchange enforces, so a later change to an asset
// does not alter a credential already issued. This is the only time the secret exists in
// clear: the database keeps its hash. The credential stays valid until it expires or
// RevokeForDeployment is called.
func (s *CredentialService) ProvisionWithGrants(ctx context.Context, caller, deploymentID string, grants map[string][]string) (string, error) {
	if err := s.ValidateGrants(grants); err != nil {
		return "", fmt.Errorf("service %q: %w", caller, err)
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
		Grants:       grants,
		ExpiresAt:    time.Now().Add(s.ttl),
	}, credentialRetention); err != nil {
		return "", err
	}

	logger.InfofCtx(ctx, "service credential created: id=%s caller=%s deployment=%s", id, caller, deploymentID)

	return id.String() + "." + secret, nil
}

// RevokeForDeployment revokes every active credential of a deployment. Call it when the
// service the deployment belongs to has been deleted, so its credentials stop working at
// once and not when they expire.
func (s *CredentialService) RevokeForDeployment(ctx context.Context, deploymentID string) error {
	n, err := s.repo.RevokeByDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}
	if n > 0 {
		logger.InfofCtx(ctx, "service credentials revoked: deployment=%s count=%d", deploymentID, n)
	}

	return nil
}

// Exchange trades a credential for a short-lived token for one service. The audience must
// be in the grants stored with the credential. Every failure returns ErrInvalidCredential,
// except an unexpected storage error.
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

	roles, granted := rec.Grants[service]
	if !granted || len(roles) == 0 || slices.Contains(roles, RoleAdmin) {
		logger.WarningfCtx(ctx, "service credential %s (caller %s) asked for a token it is not granted: aud=%s", id, rec.Caller, service)

		return "", time.Time{}, ErrInvalidCredential
	}

	token, exp, err := s.tokens.issueFixed(ctx, rec.Caller, service, roles)
	if errors.Is(err, ErrUnknownService) {
		return "", time.Time{}, ErrInvalidCredential
	}

	return token, exp, err
}
