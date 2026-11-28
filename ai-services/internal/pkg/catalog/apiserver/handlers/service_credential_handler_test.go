package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/handlers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

const credentialPath = "/api/v1/auth/service-credential/token"

type credRepo struct {
	mu  sync.Mutex
	m   map[uuid.UUID]*dbmodels.ServiceCredential
	err error // returned by GetValid when set
}

func (f *credRepo) Insert(_ context.Context, c *dbmodels.ServiceCredential, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.m[c.ID] = &cp

	return nil
}

func (f *credRepo) GetValid(_ context.Context, id uuid.UUID) (*dbmodels.ServiceCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.m[id]
	if !ok || c.RevokedAt != nil || !c.ExpiresAt.After(time.Now()) {
		return nil, dbrepo.ErrServiceCredentialNotFound
	}
	cp := *c

	return &cp, nil
}

func (f *credRepo) RevokeByDeployment(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func newCredentialEngine(t *testing.T, withCreds bool) (*gin.Engine, *auth.CredentialService, *credRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	signer, err := auth.NewSigner(context.Background(), &keyRepo{}, "enc-key")
	require.NoError(t, err)
	users := repository.NewInMemoryUserRepoWithAdminHash(adminID, "admin", "Admin", "x")
	reg := auth.DefaultServiceRegistry()
	ts, err := auth.NewTokenService(users, auth.NewBootstrapAdminResolver(adminID, reg), reg, signer,
		&codeRepo{m: map[string]*dbmodels.AuthCode{}}, targets{}, 15*time.Minute)
	require.NoError(t, err)

	repo := &credRepo{m: map[uuid.UUID]*dbmodels.ServiceCredential{}}
	cs, err := auth.NewCredentialService(ts, repo, time.Hour)
	require.NoError(t, err)

	h := handlers.NewServiceTokenHandler(ts, signer)
	if withCreds {
		h.WithCredentials(cs)
	}
	r := gin.New()
	r.POST(credentialPath, h.CredentialToken)

	return r, cs, repo
}

func postCredential(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, credentialPath, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func credBody(t *testing.T, credential, service string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"credential": credential, "service": service})
	require.NoError(t, err)

	return string(b)
}

func TestCredentialToken_Success(t *testing.T) {
	r, cs, _ := newCredentialEngine(t, true)
	cred, err := cs.ProvisionWithGrants(context.Background(), "chat", "dep-1", map[string][]string{"similarity": {auth.RoleRead}})
	require.NoError(t, err)

	w := postCredential(r, credBody(t, cred, "similarity"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var resp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, strings.Split(resp.Token, "."), 3)
	assert.NotEmpty(t, resp.ExpiresAt)
}

func TestCredentialToken_EveryFailureIs401WithTheSameBody(t *testing.T) {
	r, cs, _ := newCredentialEngine(t, true)
	cred, err := cs.ProvisionWithGrants(context.Background(), "chat", "dep-1", map[string][]string{"similarity": {auth.RoleRead}})
	require.NoError(t, err)
	idText, _, _ := strings.Cut(cred, ".")

	bodies := map[string]string{
		"wrong secret":     credBody(t, idText+".AAAA", "similarity"),
		"unknown id":       credBody(t, uuid.NewString()+".AAAA", "similarity"),
		"not granted":      credBody(t, cred, "digitize"),
		"unknown audience": credBody(t, cred, "nope"),
		"missing service":  `{"credential":"` + cred + `"}`,
		"missing cred":     `{"service":"similarity"}`,
		"not json":         `not json`,
		"empty":            ``,
	}
	var first string
	for name, body := range bodies {
		w := postCredential(r, body)
		assert.Equal(t, http.StatusUnauthorized, w.Code, name)
		assert.JSONEq(t, `{"error":"invalid credential"}`, w.Body.String(), name)
		if first == "" {
			first = w.Body.String()
		}
		assert.Equal(t, first, w.Body.String(), name)
	}
}

func TestCredentialToken_StorageErrorIsGeneric500(t *testing.T) {
	r, cs, repo := newCredentialEngine(t, true)
	cred, err := cs.ProvisionWithGrants(context.Background(), "chat", "dep-1", map[string][]string{"similarity": {auth.RoleRead}})
	require.NoError(t, err)
	repo.err = errors.New("pq: connection refused to 10.0.0.5")

	w := postCredential(r, credBody(t, cred, "similarity"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, w.Body.String())
	assert.NotContains(t, w.Body.String(), "10.0.0.5")
}

func TestCredentialToken_DisabledIs404(t *testing.T) {
	r, _, _ := newCredentialEngine(t, false)
	w := postCredential(r, credBody(t, uuid.NewString()+".AAAA", "similarity"))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
