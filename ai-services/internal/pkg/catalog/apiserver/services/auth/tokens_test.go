package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

const (
	testEncKey = "test-db-encryption-key"
	adminID    = "uid_1"
	digitizeUI = "https://digitize-ui-abc.example.com"
	callback   = digitizeUI + "/auth/callback"
)

// ---- in-memory fakes of the DB repositories ----

type fakeKeyRepo struct {
	mu   sync.Mutex
	keys []dbmodels.SigningKey
}

func (f *fakeKeyRepo) EnsureActive(_ context.Context, newKey func() (*dbmodels.SigningKey, error)) (*dbmodels.SigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.keys {
		if f.keys[i].RetiredAt == nil {
			k := f.keys[i]
			return &k, nil
		}
	}
	k, err := newKey()
	if err != nil {
		return nil, err
	}
	k.CreatedAt = time.Now()
	f.keys = append(f.keys, *k)
	return k, nil
}

func (f *fakeKeyRepo) ListPublishable(_ context.Context, _ time.Duration) ([]dbmodels.SigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dbmodels.SigningKey(nil), f.keys...), nil
}

type fakeCodeRepo struct {
	mu    sync.Mutex
	codes map[string]*dbmodels.AuthCode
}

func newFakeCodeRepo() *fakeCodeRepo { return &fakeCodeRepo{codes: map[string]*dbmodels.AuthCode{}} }

func (f *fakeCodeRepo) Insert(_ context.Context, c *dbmodels.AuthCode, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.codes[c.CodeHash] = &cp
	return nil
}

func (f *fakeCodeRepo) Consume(_ context.Context, h string) (*dbmodels.AuthCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[h]
	if !ok || c.UsedAt != nil || !c.ExpiresAt.After(time.Now()) {
		return nil, dbrepo.ErrAuthCodeNotFound
	}
	now := time.Now()
	c.UsedAt = &now
	cp := *c
	return &cp, nil
}

func (f *fakeCodeRepo) WasUsed(_ context.Context, h string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[h]
	return ok && c.UsedAt != nil, nil
}

type fakeTargets struct{ urls map[string][]string }

func (f *fakeTargets) UIEndpoints(_ context.Context, id string) ([]string, error) {
	return f.urls[id], nil
}

// ---- fixture ----

type fixture struct {
	svc     *auth.TokenService
	signer  *auth.Signer
	users   *repository.InMemoryUserRepo
	codes   *fakeCodeRepo
	keyRepo *fakeKeyRepo
}

func newFixture(t *testing.T, resolver func(*auth.ServiceRegistry) auth.ScopeResolver) *fixture {
	t.Helper()
	keyRepo := &fakeKeyRepo{}
	signer, err := auth.NewSigner(context.Background(), keyRepo, testEncKey)
	require.NoError(t, err)

	users := repository.NewInMemoryUserRepoWithAdminHash(adminID, "admin", "Admin", "x")
	reg := auth.DefaultServiceRegistry()
	var res auth.ScopeResolver = auth.NewBootstrapAdminResolver(adminID, reg)
	if resolver != nil {
		res = resolver(reg)
	}
	codes := newFakeCodeRepo()
	svc, err := auth.NewTokenService(users, res, reg, signer, codes,
		&fakeTargets{urls: map[string][]string{"digitize": {digitizeUI}}}, auth.DefaultServiceTokenTTL)
	require.NoError(t, err)
	return &fixture{svc: svc, signer: signer, users: users, codes: codes, keyRepo: keyRepo}
}

// verify parses a token against the JWKS the signer publishes, the way a service would.
func (f *fixture) verify(t *testing.T, raw string) (*auth.ServiceClaims, *jwt.Token) {
	t.Helper()
	set, err := f.signer.JWKS(context.Background())
	require.NoError(t, err)
	claims := &auth.ServiceClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(tk *jwt.Token) (any, error) {
		for _, k := range set.Keys {
			if k.Kid == tk.Header["kid"] {
				x, _ := base64.RawURLEncoding.DecodeString(k.X)
				y, _ := base64.RawURLEncoding.DecodeString(k.Y)
				return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
			}
		}
		return nil, assert.AnError
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithAudience("digitize"), jwt.WithIssuer(auth.ServiceTokenIssuer))
	require.NoError(t, err)
	return claims, tok
}

func pkce(t *testing.T) (verifier, challenge string) {
	t.Helper()
	verifier = strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func codeFrom(t *testing.T, redirectTo string) (code, state string) {
	t.Helper()
	require.True(t, strings.HasPrefix(redirectTo, callback+"?"), redirectTo)
	q := strings.SplitN(redirectTo, "?", 2)[1]
	for _, kv := range strings.Split(q, "&") {
		p := strings.SplitN(kv, "=", 2)
		switch p[0] {
		case "code":
			code = p[1]
		case "state":
			state = p[1]
		}
	}
	return
}

// ---- signer ----

func TestSigner_FirstStartCreatesOneKeyAndRestartReusesIt(t *testing.T) {
	repo := &fakeKeyRepo{}
	a, err := auth.NewSigner(context.Background(), repo, testEncKey)
	require.NoError(t, err)
	b, err := auth.NewSigner(context.Background(), repo, testEncKey)
	require.NoError(t, err)

	require.Len(t, repo.keys, 1)
	sa, _ := a.Sign(auth.ServiceClaims{})
	sb, _ := b.Sign(auth.ServiceClaims{})
	ta, _, _ := jwt.NewParser().ParseUnverified(sa, &auth.ServiceClaims{})
	tb, _, _ := jwt.NewParser().ParseUnverified(sb, &auth.ServiceClaims{})
	assert.Equal(t, ta.Header["kid"], tb.Header["kid"])
}

func TestSigner_PrivateKeyIsNotStoredInPlaintext(t *testing.T) {
	repo := &fakeKeyRepo{}
	_, err := auth.NewSigner(context.Background(), repo, testEncKey)
	require.NoError(t, err)
	assert.NotContains(t, repo.keys[0].PrivateKeyEnc, "PRIVATE KEY")
	assert.NotContains(t, repo.keys[0].PublicKeyPEM, "PRIVATE")
}

func TestSigner_WrongEncryptionKeyFails(t *testing.T) {
	repo := &fakeKeyRepo{}
	_, err := auth.NewSigner(context.Background(), repo, testEncKey)
	require.NoError(t, err)
	_, err = auth.NewSigner(context.Background(), repo, "a-different-key")
	require.Error(t, err)
}

func TestSigner_RequiresEncryptionKey(t *testing.T) {
	_, err := auth.NewSigner(context.Background(), &fakeKeyRepo{}, "")
	require.Error(t, err)
}

func TestSigner_JWKSHasNoPrivateMember(t *testing.T) {
	f := newFixture(t, nil)
	set, err := f.signer.JWKS(context.Background())
	require.NoError(t, err)
	require.Len(t, set.Keys, 1)
	k := set.Keys[0]
	assert.Equal(t, "EC", k.Kty)
	assert.Equal(t, "P-256", k.Crv)
	assert.Equal(t, "ES256", k.Alg)
	assert.NotEmpty(t, k.X)
	assert.NotEmpty(t, k.Y)
	// JWK has no field that could carry "d"; also check the serialised form.
	raw, _ := jsonString(set)
	assert.NotContains(t, raw, `"d"`)
}

// ---- service-token ----

func TestIssueServiceToken_AdminGetsDigitizeAdmin(t *testing.T) {
	f := newFixture(t, nil)
	raw, exp, err := f.svc.IssueServiceToken(context.Background(), adminID, "digitize")
	require.NoError(t, err)

	claims, tok := f.verify(t, raw)
	assert.Equal(t, "ES256", tok.Method.Alg())
	assert.Equal(t, "digitize:admin", claims.Scope)
	assert.Equal(t, adminID, claims.Subject)
	assert.Equal(t, "local", claims.IDP)
	assert.Equal(t, "user", claims.Type)
	assert.NotEmpty(t, claims.ID)
	assert.WithinDuration(t, time.Now().Add(auth.DefaultServiceTokenTTL), exp, 5*time.Second)
}

func TestIssueServiceToken_UnknownServiceRejected(t *testing.T) {
	f := newFixture(t, nil)
	_, _, err := f.svc.IssueServiceToken(context.Background(), adminID, "nope")
	assert.ErrorIs(t, err, auth.ErrUnknownService)
}

func TestIssueServiceToken_OtherPrincipalForbidden(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, f.users.Upsert(&models.User{ID: "ext-1", UserName: "bob", IDP: auth.IDPMIQ}))
	_, _, err := f.svc.IssueServiceToken(context.Background(), "ext-1", "digitize")
	assert.ErrorIs(t, err, auth.ErrForbidden)
}

func TestIssueServiceToken_UnknownUserForbidden(t *testing.T) {
	f := newFixture(t, nil)
	_, _, err := f.svc.IssueServiceToken(context.Background(), "ghost", "digitize")
	assert.ErrorIs(t, err, auth.ErrForbidden)
}

func TestIssueServiceToken_ManageIQUserWithAdminIDCannotReplaceAdmin(t *testing.T) {
	f := newFixture(t, nil)
	// Same ID as the admin, and same user name as the admin: both must be refused.
	assert.ErrorIs(t, f.users.Upsert(&models.User{ID: adminID, UserName: "mallory", IDP: auth.IDPMIQ}), repository.ErrReservedIdentity)
	assert.ErrorIs(t, f.users.Upsert(&models.User{ID: "ext-9", UserName: "admin", IDP: auth.IDPMIQ}), repository.ErrReservedIdentity)

	u, err := f.users.GetByID(context.Background(), adminID)
	require.NoError(t, err)
	assert.Equal(t, "admin", u.UserName)
	assert.Equal(t, auth.IDPLocal, u.IDP)
	assert.Equal(t, "x", u.PasswordHash)
}

func TestIssueServiceToken_SameIDWrongIDPIsForbidden(t *testing.T) {
	// The resolver keys on ID and IDP. An identity with the admin's ID but another IDP gets nothing.
	r := auth.NewBootstrapAdminResolver(adminID, auth.DefaultServiceRegistry())
	roles, err := r.Resolve(context.Background(), auth.Principal{ID: adminID, IDP: auth.IDPMIQ}, "digitize")
	require.NoError(t, err)
	assert.Empty(t, roles)
}

// Extensibility proof: a different resolver grants another user "read" with the unchanged
// endpoint logic and signer, and the production resolver refuses the same user.
type readOnlyResolver struct{ userID string }

func (r readOnlyResolver) Resolve(_ context.Context, p auth.Principal, _ string) ([]string, error) {
	if p.ID == r.userID {
		return []string{auth.RoleRead}, nil
	}
	return nil, nil
}

func TestIssueServiceToken_ResolverControlsScope(t *testing.T) {
	f := newFixture(t, func(*auth.ServiceRegistry) auth.ScopeResolver { return readOnlyResolver{userID: "user1"} })
	require.NoError(t, f.users.Upsert(&models.User{ID: "user1", UserName: "user1", IDP: auth.IDPLocal}))

	raw, _, err := f.svc.IssueServiceToken(context.Background(), "user1", "digitize")
	require.NoError(t, err)
	claims, _ := f.verify(t, raw)
	assert.Equal(t, "digitize:read", claims.Scope)
	assert.Equal(t, "user1", claims.Subject)

	prod := newFixture(t, nil)
	require.NoError(t, prod.users.Upsert(&models.User{ID: "user1", UserName: "user1", IDP: auth.IDPLocal}))
	_, _, err = prod.svc.IssueServiceToken(context.Background(), "user1", "digitize")
	assert.ErrorIs(t, err, auth.ErrForbidden)
}

type errResolver struct{}

func (errResolver) Resolve(context.Context, auth.Principal, string) ([]string, error) {
	return nil, assert.AnError
}

func TestIssueServiceToken_ResolverErrorIsNeverAToken(t *testing.T) {
	f := newFixture(t, func(*auth.ServiceRegistry) auth.ScopeResolver { return errResolver{} })
	raw, _, err := f.svc.IssueServiceToken(context.Background(), adminID, "digitize")
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrForbidden)
	assert.Empty(t, raw)
}

func TestTokenTTLValidation(t *testing.T) {
	assert.NoError(t, auth.ValidateServiceTokenTTL(15*time.Minute))
	assert.NoError(t, auth.ValidateServiceTokenTTL(time.Hour))
	assert.Error(t, auth.ValidateServiceTokenTTL(time.Hour+time.Second))
	assert.Error(t, auth.ValidateServiceTokenTTL(0))
	assert.Error(t, auth.ValidateServiceTokenTTL(-time.Minute))
}

// ---- authorize + exchange ----

func authorize(t *testing.T, f *fixture, challenge string) string {
	t.Helper()
	to, err := f.svc.Authorize(context.Background(), adminID, auth.AuthorizeRequest{
		Service: "digitize", RedirectURI: callback, CodeChallenge: challenge, State: "st-1",
	})
	require.NoError(t, err)
	return to
}

func TestAuthorizeThenExchange_Succeeds(t *testing.T) {
	f := newFixture(t, nil)
	verifier, challenge := pkce(t)
	code, state := codeFrom(t, authorize(t, f, challenge))
	assert.Equal(t, "st-1", state)

	raw, _, err := f.svc.Exchange(context.Background(), auth.ExchangeRequest{
		Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier,
	})
	require.NoError(t, err)
	claims, _ := f.verify(t, raw)
	assert.Equal(t, "digitize:admin", claims.Scope)
	assert.Equal(t, adminID, claims.Subject)
}

func TestExchange_CodeIsStoredHashed(t *testing.T) {
	f := newFixture(t, nil)
	_, challenge := pkce(t)
	code, _ := codeFrom(t, authorize(t, f, challenge))
	for h := range f.codes.codes {
		assert.NotEqual(t, code, h)
		assert.Len(t, h, 64)
	}
}

func TestExchange_SecondUseFails(t *testing.T) {
	f := newFixture(t, nil)
	verifier, challenge := pkce(t)
	code, _ := codeFrom(t, authorize(t, f, challenge))
	req := auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier}

	_, _, err := f.svc.Exchange(context.Background(), req)
	require.NoError(t, err)
	_, _, err = f.svc.Exchange(context.Background(), req)
	assert.ErrorIs(t, err, auth.ErrInvalidGrant)
}

func TestExchange_AllFailuresReturnTheSameError(t *testing.T) {
	_, challenge := pkce(t)
	good := func(f *fixture) auth.ExchangeRequest {
		verifier, _ := pkce(t)
		code, _ := codeFrom(t, authorize(t, f, challenge))
		return auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier}
	}
	cases := map[string]func(*fixture, *auth.ExchangeRequest){
		"wrong verifier": func(_ *fixture, r *auth.ExchangeRequest) { r.CodeVerifier = strings.Repeat("w", 43) },
		"short verifier": func(_ *fixture, r *auth.ExchangeRequest) { r.CodeVerifier = "short" },
		"wrong redirect": func(_ *fixture, r *auth.ExchangeRequest) { r.RedirectURI = digitizeUI + "/other" },
		"wrong service":  func(_ *fixture, r *auth.ExchangeRequest) { r.Service = "summarize" },
		"unknown code":   func(_ *fixture, r *auth.ExchangeRequest) { r.Code = "nope" },
		"empty code":     func(_ *fixture, r *auth.ExchangeRequest) { r.Code = "" },
		"expired": func(f *fixture, _ *auth.ExchangeRequest) {
			for _, c := range f.codes.codes {
				c.ExpiresAt = time.Now().Add(-time.Second)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			req := good(f)
			mutate(f, &req)
			raw, _, err := f.svc.Exchange(context.Background(), req)
			assert.ErrorIs(t, err, auth.ErrInvalidGrant)
			assert.Empty(t, raw)
		})
	}
}

func TestExchange_WrongVerifierBurnsTheCode(t *testing.T) {
	f := newFixture(t, nil)
	verifier, challenge := pkce(t)
	code, _ := codeFrom(t, authorize(t, f, challenge))

	_, _, err := f.svc.Exchange(context.Background(), auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: strings.Repeat("w", 43)})
	require.ErrorIs(t, err, auth.ErrInvalidGrant)
	_, _, err = f.svc.Exchange(context.Background(), auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier})
	assert.ErrorIs(t, err, auth.ErrInvalidGrant, "a guess must not leave the code usable")
}

func TestExchange_ConcurrentUseSucceedsExactlyOnce(t *testing.T) {
	f := newFixture(t, nil)
	verifier, challenge := pkce(t)
	code, _ := codeFrom(t, authorize(t, f, challenge))
	req := auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := f.svc.Exchange(context.Background(), req); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, ok)
}

func TestExchange_GrantRemovedBetweenAuthorizeAndExchange(t *testing.T) {
	switchable := &switchResolver{allow: true}
	f := newFixture(t, func(*auth.ServiceRegistry) auth.ScopeResolver { return switchable })
	verifier, challenge := pkce(t)
	code, _ := codeFrom(t, authorize(t, f, challenge))

	switchable.allow = false
	_, _, err := f.svc.Exchange(context.Background(), auth.ExchangeRequest{Service: "digitize", RedirectURI: callback, Code: code, CodeVerifier: verifier})
	assert.ErrorIs(t, err, auth.ErrInvalidGrant)
}

type switchResolver struct{ allow bool }

func (s *switchResolver) Resolve(context.Context, auth.Principal, string) ([]string, error) {
	if s.allow {
		return []string{auth.RoleAdmin}, nil
	}
	return nil, nil
}

func TestAuthorize_RejectsBadRequests(t *testing.T) {
	_, challenge := pkce(t)
	base := auth.AuthorizeRequest{Service: "digitize", RedirectURI: callback, CodeChallenge: challenge, State: "s"}
	cases := map[string]func(*auth.AuthorizeRequest){
		"unregistered host": func(r *auth.AuthorizeRequest) { r.RedirectURI = "https://evil.example.com/auth/callback" },
		"different path":    func(r *auth.AuthorizeRequest) { r.RedirectURI = digitizeUI + "/auth/callback2" },
		"trailing slash":    func(r *auth.AuthorizeRequest) { r.RedirectURI = callback + "/" },
		"query string":      func(r *auth.AuthorizeRequest) { r.RedirectURI = callback + "?a=b" },
		"fragment":          func(r *auth.AuthorizeRequest) { r.RedirectURI = callback + "#x" },
		"http scheme":       func(r *auth.AuthorizeRequest) { r.RedirectURI = "http://digitize-ui-abc.example.com/auth/callback" },
		"userinfo trick": func(r *auth.AuthorizeRequest) {
			r.RedirectURI = "https://digitize-ui-abc.example.com@evil.example.com/auth/callback"
		},
		"different port": func(r *auth.AuthorizeRequest) {
			r.RedirectURI = "https://digitize-ui-abc.example.com:8443/auth/callback"
		},
		"uppercase host":  func(r *auth.AuthorizeRequest) { r.RedirectURI = "https://DIGITIZE-UI-ABC.example.com/auth/callback" },
		"empty redirect":  func(r *auth.AuthorizeRequest) { r.RedirectURI = "" },
		"bad challenge":   func(r *auth.AuthorizeRequest) { r.CodeChallenge = "short" },
		"missing state":   func(r *auth.AuthorizeRequest) { r.State = "" },
		"oversized state": func(r *auth.AuthorizeRequest) { r.State = strings.Repeat("a", 257) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			req := base
			mutate(&req)
			_, err := f.svc.Authorize(context.Background(), adminID, req)
			require.Error(t, err)
			assert.Empty(t, f.codes.codes, "no code may be stored for a rejected request")
		})
	}
}

func TestAuthorize_UnknownServiceAndForbidden(t *testing.T) {
	f := newFixture(t, nil)
	_, challenge := pkce(t)
	_, err := f.svc.Authorize(context.Background(), adminID, auth.AuthorizeRequest{Service: "nope", RedirectURI: callback, CodeChallenge: challenge, State: "s"})
	assert.ErrorIs(t, err, auth.ErrUnknownService)

	require.NoError(t, f.users.Upsert(&models.User{ID: "ext-1", UserName: "bob", IDP: auth.IDPMIQ}))
	_, err = f.svc.Authorize(context.Background(), "ext-1", auth.AuthorizeRequest{Service: "digitize", RedirectURI: callback, CodeChallenge: challenge, State: "s"})
	assert.ErrorIs(t, err, auth.ErrForbidden)
}

func TestAuthorize_LoopbackHTTPAllowedOnlyWhenRegistered(t *testing.T) {
	keyRepo := &fakeKeyRepo{}
	signer, err := auth.NewSigner(context.Background(), keyRepo, testEncKey)
	require.NoError(t, err)
	users := repository.NewInMemoryUserRepoWithAdminHash(adminID, "admin", "Admin", "x")
	reg := auth.DefaultServiceRegistry()
	svc, err := auth.NewTokenService(users, auth.NewBootstrapAdminResolver(adminID, reg), reg, signer, newFakeCodeRepo(),
		&fakeTargets{urls: map[string][]string{"digitize": {"http://localhost:5173"}}}, time.Minute)
	require.NoError(t, err)

	_, challenge := pkce(t)
	_, err = svc.Authorize(context.Background(), adminID, auth.AuthorizeRequest{Service: "digitize", RedirectURI: "http://localhost:5173/auth/callback", CodeChallenge: challenge, State: "s"})
	assert.NoError(t, err)
	_, err = svc.Authorize(context.Background(), adminID, auth.AuthorizeRequest{Service: "digitize", RedirectURI: "http://localhost:6000/auth/callback", CodeChallenge: challenge, State: "s"})
	assert.ErrorIs(t, err, auth.ErrInvalidRedirect)
}

func TestNewTokenService_RejectsBadTTL(t *testing.T) {
	_, err := auth.NewTokenService(nil, nil, auth.DefaultServiceRegistry(), nil, nil, nil, 2*time.Hour)
	assert.Error(t, err)
}

// ---- cross-rejection ----

func TestServiceTokenIsRejectedByCatalogTokenManager(t *testing.T) {
	f := newFixture(t, nil)
	raw, _, err := f.svc.IssueServiceToken(context.Background(), adminID, "digitize")
	require.NoError(t, err)

	mgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", time.Minute, time.Hour)
	_, _, err = mgr.ValidateAccessToken(raw)
	assert.Error(t, err, "an ES256 service token must not be accepted as a catalog access token")
}

func TestCatalogAccessTokenIsNotAValidServiceToken(t *testing.T) {
	f := newFixture(t, nil)
	mgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", time.Minute, time.Hour)
	access, _, err := mgr.GenerateAccessToken(adminID)
	require.NoError(t, err)

	set, _ := f.signer.JWKS(context.Background())
	require.NotEmpty(t, set.Keys)
	_, err = jwt.ParseWithClaims(access, &auth.ServiceClaims{}, func(*jwt.Token) (any, error) { return nil, assert.AnError },
		jwt.WithValidMethods([]string{"ES256"}))
	assert.Error(t, err)
}
