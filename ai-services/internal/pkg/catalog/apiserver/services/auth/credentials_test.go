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

func (f *fakeCredRepo) RevokeByDeployment(_ context.Context, deploymentID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	now := time.Now()
	for _, c := range f.items {
		if c.DeploymentID == deploymentID && c.RevokedAt == nil {
			c.RevokedAt = &now
			n++
		}
	}

	return n, nil
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
// chatGrants are the grants of the chat asset's @credential annotation.
var chatGrants = map[string][]string{"similarity": {auth.RoleRead}}

func (f *credFixture) provision(ctx context.Context, caller, deploymentID string) (string, error) {
	return f.creds.ProvisionWithGrants(ctx, caller, deploymentID, chatGrants)
}

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
	cred, err := f.provision(context.Background(), "chat", "dep-1")
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
	cred, err := f.provision(context.Background(), "chat", "dep-1")
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
	cred, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	idText, _, _ := strings.Cut(cred, ".")

	// expired and revoked credentials
	expired, err := f.provision(ctx, "chat", "dep-2")
	require.NoError(t, err)
	revoked, err := f.provision(ctx, "chat", "dep-3")
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
	oldCred, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	newCred, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	assert.NotEqual(t, oldCred, newCred)

	for _, c := range []string{oldCred, newCred} {
		_, _, err := f.creds.Exchange(ctx, c, "similarity")
		assert.NoError(t, err)
	}
}

func TestCredential_GrantsAreStoredAndEnforcedFromTheRow(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()

	cred, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	idText, _, _ := strings.Cut(cred, ".")
	stored := f.repo.items[uuid.MustParse(idText)]
	assert.Equal(t, map[string][]string{"similarity": {auth.RoleRead}}, stored.Grants)

	// Exchange follows the row, not the code table: removing the grant from the row denies it,
	// and a grant that only the row has is honoured.
	stored.Grants = map[string][]string{"summarize": {auth.RoleRead}}
	_, _, err = f.creds.Exchange(ctx, cred, "similarity")
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	raw, _, err := f.creds.Exchange(ctx, cred, "summarize")
	require.NoError(t, err)
	assert.Equal(t, "summarize:read", f.verifyFor(t, raw, "summarize").Scope)
}

func TestCredential_ProvisionWithGrantsRejectsBadGrants(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()

	cases := map[string]map[string][]string{
		"nil":              nil,
		"empty":            {},
		"admin":            {"similarity": {auth.RoleAdmin}},
		"admin among many": {"similarity": {auth.RoleRead, auth.RoleAdmin}},
		"unknown audience": {"nope": {auth.RoleRead}},
		"no roles":         {"similarity": {}},
	}
	for name, grants := range cases {
		_, err := f.creds.ProvisionWithGrants(ctx, "chat", "dep", grants)
		assert.Error(t, err, name)
	}
	assert.Empty(t, f.repo.items, "nothing is stored for rejected grants")

	_, err := f.creds.ProvisionWithGrants(ctx, "chat", "dep", map[string][]string{"similarity": {auth.RoleRead}})
	assert.NoError(t, err)
}

func TestCredential_ExchangeRefusesAdminEvenIfARowHoldsIt(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()
	cred, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	idText, _, _ := strings.Cut(cred, ".")
	f.repo.items[uuid.MustParse(idText)].Grants = map[string][]string{"similarity": {auth.RoleRead, auth.RoleAdmin}}

	_, _, err = f.creds.Exchange(ctx, cred, "similarity")
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestCredential_RevokeForDeploymentRevokesOnlyThatDeployment(t *testing.T) {
	f := newCredFixture(t, time.Hour)
	ctx := context.Background()
	first, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	second, err := f.provision(ctx, "chat", "dep-1")
	require.NoError(t, err)
	other, err := f.provision(ctx, "chat", "dep-2")
	require.NoError(t, err)

	require.NoError(t, f.creds.RevokeForDeployment(ctx, "dep-1"))

	for _, c := range []string{first, second} {
		_, _, err = f.creds.Exchange(ctx, c, "similarity")
		assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	}
	_, _, err = f.creds.Exchange(ctx, other, "similarity")
	assert.NoError(t, err)
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
