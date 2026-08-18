package miq_test

// Integration tests for the ManageIQ HTTP client.
//
// These tests connect to a real ManageIQ instance.
// They are skipped automatically unless the following environment variables are set:
//
//	MIQ_URL  - base URL, e.g. https://9.20.202.144:8443
//	MIQ_USER - username, e.g. admin
//	MIQ_PASS - password, e.g. smartvm
//
// Run with:
//
//	MIQ_URL=https://9.20.202.144:8443 MIQ_USER=admin MIQ_PASS=smartvm \
//	  go test ./internal/pkg/catalog/miq/... -v -run Integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/miq"
)

// miqEnv holds the integration test configuration read from env vars.
type miqEnv struct {
	url  string
	user string
	pass string
}

// integrationEnv reads the env vars and skips the test if any are missing.
func integrationEnv(t *testing.T) miqEnv {
	t.Helper()
	url := os.Getenv("MIQ_URL")
	user := os.Getenv("MIQ_USER")
	pass := os.Getenv("MIQ_PASS")
	if url == "" || user == "" || pass == "" {
		t.Skip("skipping integration test: MIQ_URL, MIQ_USER, MIQ_PASS not set")
	}
	return miqEnv{url: url, user: user, pass: pass}
}

// acquireMIQToken performs Basic-auth against GET /api/auth and returns the token.
// Mirrors what the Catalog API does internally for Flow A.
func acquireMIQToken(t *testing.T, env miqEnv) string {
	t.Helper()
	var result struct {
		Token string `json:"auth_token"`
	}
	resp, err := resty.New().
		SetBaseURL(env.url).
		SetTLSClientConfig(cryptoTLSConfig(true)).
		R().
		SetBasicAuth(env.user, env.pass).
		SetResult(&result).
		Get("/api/auth")
	require.NoError(t, err, "acquire MIQ token: HTTP request failed")
	require.Equal(t, http.StatusOK, resp.StatusCode(),
		"acquire MIQ token: expected 200, got %d: %s", resp.StatusCode(), resp.String())
	require.NotEmpty(t, result.Token, "acquire MIQ token: auth_token is empty")
	t.Logf("acquired MIQ token: %s…", result.Token[:8])
	return result.Token
}

// ---------------------------------------------------------------------------
// Smoke test — connectivity
// ---------------------------------------------------------------------------

func TestIntegration_APIRoot_Reachable(t *testing.T) {
	env := integrationEnv(t)

	// GET /api on this ManageIQ instance requires authentication (returns 401 without
	// credentials). We verify the endpoint is reachable and returns a known error shape.
	resp, err := resty.New().
		SetBaseURL(env.url).
		SetTLSClientConfig(cryptoTLSConfig(true)).
		R().
		Get("/api")

	require.NoError(t, err, "ManageIQ API must be reachable")
	assert.Contains(t, []int{http.StatusOK, http.StatusUnauthorized}, resp.StatusCode(),
		"expected 200 or 401 from /api, got %d", resp.StatusCode())
	t.Logf("ManageIQ /api responded HTTP %d — instance reachable", resp.StatusCode())
}

// ---------------------------------------------------------------------------
// GetUserByToken — valid token
// ---------------------------------------------------------------------------

func TestIntegration_GetUserByToken_ValidToken(t *testing.T) {
	env := integrationEnv(t)
	token := acquireMIQToken(t, env)

	client := miq.NewHTTPClient(env.url, true)
	info, err := client.GetUserByToken(context.Background(), token)

	require.NoError(t, err)
	t.Logf("UserName   : %s", info.UserName)
	t.Logf("FullName   : %s", info.FullName)
	t.Logf("Groups     : %v", info.Groups)

	assert.NotEmpty(t, info.UserName)
	assert.NotEmpty(t, info.FullName)
	assert.NotEmpty(t, info.Groups)
	assert.Equal(t, env.user, info.UserName,
		"UserName must match the login username")
	assert.Contains(t, info.Groups, "EvmGroup-super_administrator",
		"admin user must belong to EvmGroup-super_administrator")
}

// ---------------------------------------------------------------------------
// GetUserByToken — invalid token
// ---------------------------------------------------------------------------

func TestIntegration_GetUserByToken_InvalidToken(t *testing.T) {
	env := integrationEnv(t)

	client := miq.NewHTTPClient(env.url, true)
	info, err := client.GetUserByToken(context.Background(), "invalid-token-value")

	assert.Nil(t, info)
	assert.ErrorIs(t, err, miq.ErrUnauthorized)
}

// ---------------------------------------------------------------------------
// GetUserByToken — revoked token
// ---------------------------------------------------------------------------

func TestIntegration_GetUserByToken_RevokedToken(t *testing.T) {
	env := integrationEnv(t)
	token := acquireMIQToken(t, env)

	// Revoke via DELETE /api/auth.
	resp, err := resty.New().
		SetBaseURL(env.url).
		SetTLSClientConfig(cryptoTLSConfig(true)).
		R().
		SetHeader("X-Auth-Token", token).
		Delete("/api/auth")
	require.NoError(t, err)
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, resp.StatusCode(),
		"logout should return 200 or 204, got %d", resp.StatusCode())
	t.Logf("token revoked (HTTP %d)", resp.StatusCode())

	client := miq.NewHTTPClient(env.url, true)
	info, err := client.GetUserByToken(context.Background(), token)

	assert.Nil(t, info)
	assert.ErrorIs(t, err, miq.ErrUnauthorized)
}

// ---------------------------------------------------------------------------
// GetUserByToken — field cross-check
// ---------------------------------------------------------------------------

func TestIntegration_GetUserByToken_UserFields(t *testing.T) {
	env := integrationEnv(t)
	token := acquireMIQToken(t, env)

	client := miq.NewHTTPClient(env.url, true)
	info, err := client.GetUserByToken(context.Background(), token)
	require.NoError(t, err)

	for _, g := range info.Groups {
		assert.NotEmpty(t, g)
		assert.Contains(t, g, "EvmGroup",
			"group %q should contain 'EvmGroup'", g)
	}
}

// ---------------------------------------------------------------------------
// Full round-trip: token → GetUserByToken → cross-check via direct API call
// ---------------------------------------------------------------------------

func TestIntegration_FullRoundTrip(t *testing.T) {
	env := integrationEnv(t)
	token := acquireMIQToken(t, env)

	client := miq.NewHTTPClient(env.url, true)
	info, err := client.GetUserByToken(context.Background(), token)
	require.NoError(t, err)

	// Cross-check: directly filter /api/users by userid and compare.
	var direct struct {
		Resources []struct {
			UserID string `json:"userid"`
		} `json:"resources"`
	}
	resp, err := resty.New().
		SetBaseURL(env.url).
		SetTLSClientConfig(cryptoTLSConfig(true)).
		R().
		SetHeader("X-Auth-Token", token).
		SetQueryParam("filter[]", fmt.Sprintf("userid=%s", info.UserName)).
		SetQueryParam("expand", "resources").
		SetResult(&direct).
		Get("/api/users")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotEmpty(t, direct.Resources)
	assert.Equal(t, info.UserName, direct.Resources[0].UserID,
		"userid from GetUserByToken must match direct /api/users lookup")
}
