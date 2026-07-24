package miq_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/miq"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// identityResponse builds the JSON body returned by GET /api?attributes=identity.
func identityResponse(userID, name, userHref string, groups []string) map[string]any {
	return map[string]any{
		"identity": map[string]any{
			"userid":    userID,
			"name":      name,
			"user_href": userHref,
			"groups":    groups,
		},
	}
}

// newStub creates a test HTTP server. handler receives full control of responses.
func newStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// ---------------------------------------------------------------------------
// Unit tests — httptest stub, no real ManageIQ required
// ---------------------------------------------------------------------------

func TestGetUserByToken_Success(t *testing.T) {
	var stub *httptest.Server
	stub = newStub(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api", r.URL.Path)
		assert.Equal(t, "valid-miq-token", r.Header.Get("X-Auth-Token"))
		assert.Equal(t, "identity", r.URL.Query().Get("attributes"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identityResponse(
			"admin",
			"Administrator",
			stub.URL+"/api/users/1",
			[]string{"EvmGroup-super_administrator"},
		))
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "valid-miq-token")

	require.NoError(t, err)
	assert.Equal(t, "admin", info.UserName)
	assert.Equal(t, "Administrator", info.FullName)
	assert.Equal(t, []string{"EvmGroup-super_administrator"}, info.Groups)
}

func TestGetUserByToken_MultipleGroups(t *testing.T) {
	var stub *httptest.Server
	stub = newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identityResponse(
			"operator1",
			"Op User",
			stub.URL+"/api/users/42",
			[]string{"EvmGroup-operator", "EvmGroup-auditor"},
		))
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "some-token")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"EvmGroup-operator", "EvmGroup-auditor"}, info.Groups)
}

func TestGetUserByToken_InvalidToken_Returns401(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"kind":    "unauthorized",
				"message": "Invalid Authentication Token bad-token specified",
				"klass":   "Api::BaseController::Authentication::AuthenticationError",
			},
		})
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "bad-token")

	assert.Nil(t, info)
	assert.ErrorIs(t, err, miq.ErrUnauthorized)
}

func TestGetUserByToken_EmptyIdentity_Returns401(t *testing.T) {
	// ManageIQ may return 200 with an empty identity block for an unknown session.
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"identity": map[string]any{}})
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "token")

	assert.Nil(t, info)
	assert.ErrorIs(t, err, miq.ErrUnauthorized)
}

func TestGetUserByToken_ServerError(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := miq.NewHTTPClient(stub.URL, false)
	_, err := client.GetUserByToken(context.Background(), "token")

	require.Error(t, err)
	assert.NotErrorIs(t, err, miq.ErrUnauthorized)
}

func TestGetUserByToken_UserHrefIDExtraction(t *testing.T) {
	// Verify the numeric ID is correctly extracted from user_href regardless of base URL.
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identityResponse(
			"testuser",
			"Test User",
			"https://9.20.202.144:8443/api/users/99",
			[]string{"EvmGroup-operator"},
		))
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "token")

	require.NoError(t, err)
	assert.Equal(t, "99", info.ExternalID)
	assert.Equal(t, "testuser", info.UserName)
}
