package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/miq"
)

// ---------------------------------------------------------------------------
// stub MIQ client
// ---------------------------------------------------------------------------

type stubMIQClient struct {
	info *miq.UserInfo
	err  error
}

func (s *stubMIQClient) GetUserByToken(_ context.Context, _ string) (*miq.UserInfo, error) {
	return s.info, s.err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newService(t *testing.T, miqClient miq.Client) auth.Service {
	t.Helper()
	tokenMgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", 15*60*1000000000 /* 15m */, 24*3600*1000000000 /* 24h */)
	users := repository.NewInMemoryUserRepo()
	return auth.NewAuthServiceWithMIQ(users, tokenMgr, &repository.NoopTokenBlacklist{}, miqClient)
}

// ---------------------------------------------------------------------------
// LoginWithToken tests
// ---------------------------------------------------------------------------

func TestLoginWithToken_Success(t *testing.T) {
	stub := &stubMIQClient{
		info: &miq.UserInfo{
			UserName: "admin",
			FullName: "Administrator",
			Groups:   []string{"EvmGroup-super_administrator"},
		},
	}
	svc := newService(t, stub)

	access, refresh, err := svc.LoginWithToken(context.Background(), "valid-miq-token")

	require.NoError(t, err)
	assert.NotEmpty(t, access, "access token must be non-empty")
	assert.NotEmpty(t, refresh, "refresh token must be non-empty")
	assert.NotEqual(t, access, refresh, "access and refresh tokens must differ")
}

func TestLoginWithToken_InvalidMIQToken(t *testing.T) {
	stub := &stubMIQClient{err: miq.ErrUnauthorized}
	svc := newService(t, stub)

	access, refresh, err := svc.LoginWithToken(context.Background(), "bad-token")

	assert.Empty(t, access)
	assert.Empty(t, refresh)
	assert.ErrorIs(t, err, miq.ErrUnauthorized)
}

func TestLoginWithToken_MIQClientNotConfigured(t *testing.T) {
	tokenMgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", 15*60*1000000000, 24*3600*1000000000)
	users := repository.NewInMemoryUserRepo()
	// NewAuthService (no MIQ client) — LoginWithToken must return an error.
	svc := auth.NewAuthService(users, tokenMgr, &repository.NoopTokenBlacklist{})

	_, _, err := svc.LoginWithToken(context.Background(), "any-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestLoginWithToken_MIQClientError(t *testing.T) {
	stub := &stubMIQClient{err: errors.New("connection refused")}
	svc := newService(t, stub)

	_, _, err := svc.LoginWithToken(context.Background(), "token")

	require.Error(t, err)
	assert.EqualError(t, err, "connection refused")
}

// A ManageIQ identity whose external ID or user name matches the bootstrap admin must be
// refused and must not replace the admin (design doc, risk 1).
func TestLoginWithToken_CannotReplaceBootstrapAdmin(t *testing.T) {
	cases := map[string]*miq.UserInfo{
		"same external id": {ExternalID: "uid_1", UserName: "mallory", FullName: "M"},
		"same user name":   {ExternalID: "ext-77", UserName: "admin", FullName: "M"},
	}
	for name, info := range cases {
		t.Run(name, func(t *testing.T) {
			tokenMgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", 15*60*1000000000, 24*3600*1000000000)
			users := repository.NewInMemoryUserRepoWithAdminHash("uid_1", "admin", "Admin", "orig-hash")
			svc := auth.NewAuthServiceWithMIQ(users, tokenMgr, &repository.NoopTokenBlacklist{}, &stubMIQClient{info: info})

			access, refresh, err := svc.LoginWithToken(context.Background(), "tok")

			assert.ErrorIs(t, err, repository.ErrReservedIdentity)
			assert.Empty(t, access)
			assert.Empty(t, refresh)

			admin, err := users.GetByID(context.Background(), "uid_1")
			require.NoError(t, err)
			assert.Equal(t, "admin", admin.UserName)
			assert.Equal(t, "orig-hash", admin.PasswordHash)
			assert.Equal(t, auth.IDPLocal, admin.IDP)
			byName, err := users.GetByUserName(context.Background(), "admin")
			require.NoError(t, err)
			assert.Equal(t, "uid_1", byName.ID)
		})
	}
}

func TestLoginWithToken_SetsMIQIDPOnSeededUser(t *testing.T) {
	stub := &stubMIQClient{info: &miq.UserInfo{ExternalID: "42", UserName: "operator1", FullName: "Op"}}
	tokenMgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", 15*60*1000000000, 24*3600*1000000000)
	users := repository.NewInMemoryUserRepo()
	svc := auth.NewAuthServiceWithMIQ(users, tokenMgr, &repository.NoopTokenBlacklist{}, stub)

	_, _, err := svc.LoginWithToken(context.Background(), "t")
	require.NoError(t, err)
	u, err := svc.GetUser(context.Background(), "42")
	require.NoError(t, err)
	assert.Equal(t, auth.IDPMIQ, u.IDP)
}

func TestLoginWithToken_UserSeededInRepo(t *testing.T) {
	stub := &stubMIQClient{
		info: &miq.UserInfo{
			ExternalID: "42",
			UserName:   "operator1",
			FullName:   "Op User",
			Groups:     []string{"EvmGroup-operator"},
		},
	}
	tokenMgr := auth.NewTokenManager("test-secret-32-bytes-long-enough!", 15*60*1000000000, 24*3600*1000000000)
	users := repository.NewInMemoryUserRepo()
	svc := auth.NewAuthServiceWithMIQ(users, tokenMgr, &repository.NoopTokenBlacklist{}, stub)

	_, _, err := svc.LoginWithToken(context.Background(), "valid-token")
	require.NoError(t, err)

	// After LoginWithToken the user must be findable via GetUser so /auth/me works.
	u, err := svc.GetUser(context.Background(), "42")
	require.NoError(t, err)
	assert.Equal(t, "42", u.ID)
	assert.Equal(t, "operator1", u.UserName)
	assert.Equal(t, "Op User", u.Name)
}
