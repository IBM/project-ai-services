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

// usersResponse builds the JSON body returned by GET /api/users.
func usersResponse(id, userid, name string, groups []string) map[string]any {
	miqGroups := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		miqGroups = append(miqGroups, map[string]any{"description": g})
	}
	return map[string]any{
		"resources": []map[string]any{
			{
				"id":         id,
				"userid":     userid,
				"name":       name,
				"miq_groups": miqGroups,
			},
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
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/users", r.URL.Path)
		assert.Equal(t, "valid-miq-token", r.Header.Get("X-Auth-Token"))
		assert.Equal(t, "resources", r.URL.Query().Get("expand"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse("1", "admin", "Administrator", []string{
			"EvmGroup-super_administrator",
		}))
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "valid-miq-token")

	require.NoError(t, err)
	assert.Equal(t, "1", info.ExternalID)
	assert.Equal(t, "admin", info.UserName)
	assert.Equal(t, "Administrator", info.FullName)
	assert.Equal(t, []string{"EvmGroup-super_administrator"}, info.Groups)
}

func TestGetUserByToken_MultipleGroups(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse("42", "operator1", "Op User", []string{
			"EvmGroup-operator",
			"EvmGroup-auditor",
		}))
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "some-token")

	require.NoError(t, err)
	assert.Equal(t, "42", info.ExternalID)
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

func TestGetUserByToken_EmptyResourceList_Returns401(t *testing.T) {
	// ManageIQ may return 200 with an empty resources array for an unknown user.
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"resources": []any{}})
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

func TestGetUserByToken_GroupsWithEmptyDescriptionSkipped(t *testing.T) {
	stub := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"resources": []map[string]any{
				{
					"id":     "5",
					"userid": "testuser",
					"name":   "Test User",
					"miq_groups": []map[string]any{
						{"description": "EvmGroup-operator"},
						{"description": ""},  // should be skipped
					},
				},
			},
		})
	})

	client := miq.NewHTTPClient(stub.URL, false)
	info, err := client.GetUserByToken(context.Background(), "token")

	require.NoError(t, err)
	assert.Equal(t, []string{"EvmGroup-operator"}, info.Groups)
}
