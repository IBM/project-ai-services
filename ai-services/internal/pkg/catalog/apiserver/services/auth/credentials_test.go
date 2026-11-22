package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// fakeCredRepo is an in-memory ServiceCredentialRepository that applies the same validity
// rule as the SQL (not revoked, not expired).
type fakeCredRepo struct {
	mu    sync.Mutex
	items map[uuid.UUID]*dbmodels.ServiceCredential
}

func newFakeCredRepo() *fakeCredRepo {
	return &fakeCredRepo{items: map[uuid.UUID]*dbmodels.ServiceCredential{}}
}

func (f *fakeCredRepo) Insert(_ context.Context, c *dbmodels.ServiceCredential, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.items[c.ID] = &cp

	return nil
}

func (f *fakeCredRepo) GetValid(_ context.Context, id uuid.UUID) (*dbmodels.ServiceCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.items[id]
	if !ok || c.RevokedAt != nil || !c.ExpiresAt.After(time.Now()) {
		return nil, dbrepo.ErrServiceCredentialNotFound
	}
	cp := *c

	return &cp, nil
}

type credFixture struct {
	*fixture
	creds *auth.CredentialService
	repo  *fakeCredRepo
}

func newCredFixture(t *testing.T, ttl time.Duration) *credFixture {
	t.Helper()
	f := newFixture(t, nil)
	repo := newFakeCredRepo()
	cs, err := auth.NewCredentialService(f.svc, repo, ttl)
	require.NoError(t, err)

	return &credFixture{fixture: f, creds: cs, repo: repo}
}

// verifyFor parses a token against the signer's JWKS for the given audience.
func (f *credFixture) verifyFor(t *testing.T, raw, aud string) *auth.ServiceClaims {
	t.Helper()
	set, err := f.signer.JWKS(context.Background())
	require.NoError(t, err)
	claims := &auth.ServiceClaims{}
	_, err = jwt.ParseWithClaims(raw, claims, func(tk *jwt.Token) (any, error) {
		for _, k := range set.Keys {
			if k.Kid == tk.Header["kid"] {
				x, _ := base64.RawURLEncoding.DecodeString(k.X)
				y, _ := base64.RawURLEncoding.DecodeString(k.Y)

				return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
			}
		}

		return nil, assert.AnError
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithAudience(aud), jwt.WithIssuer(auth.ServiceTokenIssuer))
	require.NoError(t, err)

	return claims
}

func TestCredential_ChatTradesForSimilarityReadToken(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	cred, err := f.creds.Provision(context.Background(), "chat", "dep-1")
	require.NoError(t, err)

	raw, exp, err := f.creds.Exchange(context.Background(), cred, "similarity")
	require.NoError(t, err)

	claims := f.verifyFor(t, raw, "similarity")
	assert.Equal(t, "similarity:read", claims.Scope)
	assert.NotContains(t, claims.Scope, "admin")
	assert.Equal(t, "service", claims.Type)
	assert.Equal(t, auth.IDPInternal, claims.IDP)
	assert.Equal(t, "chat", claims.Subject)
	assert.Equal(t, []string{"similarity"}, []string(claims.Audience))
	assert.WithinDuration(t, time.Now().Add(auth.DefaultServiceTokenTTL), exp, 5*time.Second)
}

func TestCredential_OnlyTheHashIsStored(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	cred, err := f.creds.Provision(context.Background(), "chat", "dep-1")
	require.NoError(t, err)

	idText, secret, ok := strings.Cut(cred, ".")
	require.True(t, ok)
	id, err := uuid.Parse(idText)
	require.NoError(t, err)

	stored := f.repo.items[id]
	require.NotNil(t, stored)
	assert.Equal(t, "chat", stored.Caller)
	assert.Equal(t, "dep-1", stored.DeploymentID)
	assert.NotContains(t, stored.SecretHash, secret)
	assert.Len(t, stored.SecretHash, 64, "hex SHA-256")
	assert.WithinDuration(t, time.Now().Add(time.Hour), stored.ExpiresAt, 5*time.Second)
}

func TestCredential_EveryFailureIsTheSameError(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()
	cred, err := f.creds.Provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	idText, _, _ := strings.Cut(cred, ".")

	// expired and revoked credentials
	expired, err := f.creds.Provision(ctx, "chat", "dep-2")
	require.NoError(t, err)
	revoked, err := f.creds.Provision(ctx, "chat", "dep-3")
	require.NoError(t, err)
	expID, _, _ := strings.Cut(expired, ".")
	revID, _, _ := strings.Cut(revoked, ".")
	f.repo.items[uuid.MustParse(expID)].ExpiresAt = time.Now().Add(-time.Minute)
	now := time.Now()
	f.repo.items[uuid.MustParse(revID)].RevokedAt = &now

	cases := map[string]struct{ credential, service string }{
		"wrong secret":      {idText + ".AAAA", "similarity"},
		"unknown id":        {uuid.NewString() + ".AAAA", "similarity"},
		"malformed":         {"not-a-credential", "similarity"},
		"empty":             {"", "similarity"},
		"empty secret":      {idText + ".", "similarity"},
		"expired":           {expired, "similarity"},
		"revoked":           {revoked, "similarity"},
		"digitize audience": {cred, "digitize"},
		"own audience":      {cred, "chat"},
		"unknown audience":  {cred, "nope"},
		"empty audience":    {cred, ""},
	}
	for name, c := range cases {
		_, _, err := f.creds.Exchange(ctx, c.credential, c.service)
		assert.ErrorIs(t, err, auth.ErrInvalidCredential, name)
	}
}

func TestCredential_TwoActiveCredentialsBothWork(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()
	oldCred, err := f.creds.Provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	newCred, err := f.creds.Provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	assert.NotEqual(t, oldCred, newCred)

	for _, c := range []string{oldCred, newCred} {
		_, _, err := f.creds.Exchange(ctx, c, "similarity")
		assert.NoError(t, err)
	}
}

func TestCredential_AdminIsNeverGranted(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()
	for _, svc := range []string{"chat", "similarity", "summarize", "digitize"} {
		cred, err := f.creds.Provision(ctx, svc, "dep")
		if !auth.CanHoldCredential(svc) {
			assert.Error(t, err, svc)

			continue
		}
		require.NoError(t, err)
		for _, aud := range []string{"chat", "similarity", "summarize", "digitize"} {
			raw, _, err := f.creds.Exchange(ctx, cred, aud)
			if err != nil {
				continue
			}
			claims := f.verifyFor(t, raw, aud)
			assert.NotContains(t, claims.Scope, "admin", "%s -> %s", svc, aud)
		}
	}
}

func TestCredential_ProvisionIfNeeded(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()

	for _, svc := range []string{"similarity", "summarize", "digitize", "nope", ""} {
		got, err := f.creds.ProvisionIfNeeded(ctx, svc, "dep")
		require.NoError(t, err, svc)
		assert.Empty(t, got, svc)
	}
	assert.Empty(t, f.repo.items, "nothing is stored for a service that cannot hold a credential")

	got, err := f.creds.ProvisionIfNeeded(ctx, "chat", "dep")
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.Len(t, f.repo.items, 1)
}

func TestCredential_ProvisionRefusesServicesThatCannotHoldOne(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	_, err := f.creds.Provision(context.Background(), "digitize", "dep")
	assert.Error(t, err)
}

func TestNewCredentialService_RejectsBadTTL(t *testing.T) {
	f := newFixture(t, nil)
	for _, ttl := range []time.Duration{0, -time.Hour} {
		_, err := auth.NewCredentialService(f.svc, newFakeCredRepo(), ttl)
		assert.Error(t, err, ttl.String())
		assert.Error(t, auth.ValidateServiceCredentialTTL(ttl), ttl.String())
	}
	assert.NoError(t, auth.ValidateServiceCredentialTTL(auth.DefaultServiceCredentialTTL))
	assert.Equal(t, 365*24*time.Hour, auth.DefaultServiceCredentialTTL)
}
