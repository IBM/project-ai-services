package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

const (
	adminID  = "uid_1"
	uiURL    = "https://digitize-ui-abc.example.com"
	callback = uiURL + "/auth/callback"
	secret   = "test-secret-32-bytes-long-enough!"
)

type keyRepo struct {
	mu   sync.Mutex
	keys []dbmodels.SigningKey
}

func (f *keyRepo) EnsureActive(_ context.Context, nk func() (*dbmodels.SigningKey, error)) (*dbmodels.SigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keys) > 0 {
		k := f.keys[0]
		return &k, nil
	}
	k, err := nk()
	if err != nil {
		return nil, err
	}
	f.keys = append(f.keys, *k)
	return k, nil
}

func (f *keyRepo) ListPublishable(context.Context, time.Duration) ([]dbmodels.SigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dbmodels.SigningKey(nil), f.keys...), nil
}

type codeRepo struct {
	mu sync.Mutex
	m  map[string]*dbmodels.AuthCode
}

func (f *codeRepo) Insert(_ context.Context, c *dbmodels.AuthCode, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.m[c.CodeHash] = &cp
	return nil
}

func (f *codeRepo) Consume(_ context.Context, h string) (*dbmodels.AuthCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.m[h]
	if !ok || c.UsedAt != nil || !c.ExpiresAt.After(time.Now()) {
		return nil, dbrepo.ErrAuthCodeNotFound
	}
	n := time.Now()
	c.UsedAt = &n
	cp := *c
	return &cp, nil
}

func (f *codeRepo) WasUsed(context.Context, string) (bool, error) { return false, nil }

type targets struct{}

func (targets) UIEndpoints(context.Context, string) ([]string, error) { return []string{uiURL}, nil }

type env struct {
	r       *gin.Engine
	tokens  *auth.TokenManager
	users   *repository.InMemoryUserRepo
	access  string // admin catalog access token
	signer  *auth.Signer
	service string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)

	signer, err := auth.NewSigner(context.Background(), &keyRepo{}, "enc-key")
	require.NoError(t, err)
	users := repository.NewInMemoryUserRepoWithAdminHash(adminID, "admin", "Admin", "x")
	reg := auth.DefaultServiceRegistry()
	svc, err := auth.NewTokenService(users, auth.NewBootstrapAdminResolver(adminID, reg), reg, signer,
		&codeRepo{m: map[string]*dbmodels.AuthCode{}}, targets{}, 15*time.Minute)
	require.NoError(t, err)
	h := handlers.NewServiceTokenHandler(svc, signer)

	mgr := auth.NewTokenManager(secret, time.Minute, time.Hour)
	authMw := middleware.AuthMiddleware(mgr, &repository.NoopTokenBlacklist{})

	r := gin.New()
	r.GET("/.well-known/jwks.json", h.JWKS)
	r.POST("/api/v1/auth/service-token", authMw, h.ServiceToken)
	r.POST("/api/v1/auth/authorize", authMw, h.Authorize)
	r.POST("/api/v1/auth/code/exchange", h.Exchange)
	r.GET("/api/v1/guarded", authMw, func(c *gin.Context) { c.Status(http.StatusOK) })

	access, _, err := mgr.GenerateAccessToken(adminID)
	require.NoError(t, err)
	return &env{r: r, tokens: mgr, users: users, access: access, signer: signer}
}

func (e *env) do(method, path, bearer string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func TestJWKS_PublicAndCacheable(t *testing.T) {
	e := newEnv(t)
	w := e.do(http.MethodGet, "/.well-known/jwks.json", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=300", w.Header().Get("Cache-Control"))
	assert.NotContains(t, w.Body.String(), `"d"`)
	var set auth.JWKS
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &set))
	require.Len(t, set.Keys, 1)
	assert.Equal(t, "ES256", set.Keys[0].Alg)
}

func TestServiceToken_Statuses(t *testing.T) {
	e := newEnv(t)

	assert.Equal(t, http.StatusUnauthorized, e.do(http.MethodPost, "/api/v1/auth/service-token", "", map[string]string{"service": "digitize"}).Code, "no token")

	refresh, _, _ := e.tokens.GenerateRefreshToken(adminID)
	assert.Equal(t, http.StatusUnauthorized, e.do(http.MethodPost, "/api/v1/auth/service-token", refresh, map[string]string{"service": "digitize"}).Code, "refresh token")

	assert.Equal(t, http.StatusBadRequest, e.do(http.MethodPost, "/api/v1/auth/service-token", e.access, map[string]string{"service": "nope"}).Code, "unknown service")
	assert.Equal(t, http.StatusBadRequest, e.do(http.MethodPost, "/api/v1/auth/service-token", e.access, map[string]string{}).Code, "missing service")

	w := e.do(http.MethodPost, "/api/v1/auth/service-token", e.access, map[string]string{"service": "digitize"})
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct{ Token, ExpiresAt string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &struct {
		Token     *string `json:"token"`
		ExpiresAt *string `json:"expires_at"`
	}{&resp.Token, &resp.ExpiresAt}))
	assert.Equal(t, 3, len(strings.Split(resp.Token, ".")))
	assert.NotEmpty(t, resp.ExpiresAt)
}

func TestServiceToken_NonAdminForbidden(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.users.Upsert(&models.User{ID: "ext-1", UserName: "bob", IDP: auth.IDPMIQ}))
	other, _, _ := e.tokens.GenerateAccessToken("ext-1")
	assert.Equal(t, http.StatusForbidden, e.do(http.MethodPost, "/api/v1/auth/service-token", other, map[string]string{"service": "digitize"}).Code)
}

// A service token must not open catalog endpoints.
func TestServiceTokenIsRejectedByCatalogMiddleware(t *testing.T) {
	e := newEnv(t)
	w := e.do(http.MethodPost, "/api/v1/auth/service-token", e.access, map[string]string{"service": "digitize"})
	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	assert.Equal(t, http.StatusUnauthorized, e.do(http.MethodGet, "/api/v1/guarded", resp.Token, nil).Code)
	assert.Equal(t, http.StatusOK, e.do(http.MethodGet, "/api/v1/guarded", e.access, nil).Code)
}

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestAuthorizeAndExchange_HTTP(t *testing.T) {
	e := newEnv(t)
	verifier := strings.Repeat("v", 43)
	authz := map[string]string{"service": "digitize", "redirect_uri": callback, "code_challenge": challenge(verifier), "code_challenge_method": "S256", "state": "st"}

	assert.Equal(t, http.StatusUnauthorized, e.do(http.MethodPost, "/api/v1/auth/authorize", "", authz).Code)

	plain := map[string]string{"service": "digitize", "redirect_uri": callback, "code_challenge": challenge(verifier), "code_challenge_method": "plain", "state": "st"}
	assert.Equal(t, http.StatusBadRequest, e.do(http.MethodPost, "/api/v1/auth/authorize", e.access, plain).Code, "plain PKCE")

	bad := map[string]string{"service": "digitize", "redirect_uri": "https://evil.example.com/auth/callback", "code_challenge": challenge(verifier), "code_challenge_method": "S256", "state": "st"}
	assert.Equal(t, http.StatusBadRequest, e.do(http.MethodPost, "/api/v1/auth/authorize", e.access, bad).Code, "unregistered redirect")

	w := e.do(http.MethodPost, "/api/v1/auth/authorize", e.access, authz)
	require.Equal(t, http.StatusOK, w.Code)
	var az struct {
		RedirectTo string `json:"redirect_to"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &az))
	require.True(t, strings.HasPrefix(az.RedirectTo, callback+"?code="), az.RedirectTo)
	code := strings.TrimPrefix(strings.SplitN(az.RedirectTo, "&", 2)[0], callback+"?code=")

	ex := map[string]string{"service": "digitize", "redirect_uri": callback, "code": code, "code_verifier": verifier}
	w = e.do(http.MethodPost, "/api/v1/auth/code/exchange", "", ex)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	// Second use and a wrong verifier give the identical response.
	w2 := e.do(http.MethodPost, "/api/v1/auth/code/exchange", "", ex)
	ex["code_verifier"] = strings.Repeat("w", 43)
	w3 := e.do(http.MethodPost, "/api/v1/auth/code/exchange", "", ex)
	assert.Equal(t, http.StatusBadRequest, w2.Code)
	assert.Equal(t, w2.Body.String(), w3.Body.String())
	assert.Equal(t, http.StatusBadRequest, e.do(http.MethodPost, "/api/v1/auth/code/exchange", "", map[string]string{}).Code)
	assert.Contains(t, w2.Body.String(), "invalid grant")
}
