package common_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

// fakeJWT returns an unsigned token with the given exp. Only the exp claim is read by the helper.
func fakeJWT(exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload := fmt.Sprintf(`{"exp":%d}`, exp.Unix())

	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

// useTokenSource replaces the token source for one test and restores it afterwards.
func useTokenSource(t *testing.T, src func(context.Context, string) (string, error)) {
	t.Helper()

	orig := common.ServiceTokenSource
	common.ServiceTokenSource = src
	common.ResetServiceTokenCache()

	t.Cleanup(func() {
		common.ServiceTokenSource = orig
		common.ResetServiceTokenCache()
	})
}

func TestServiceToken_IsPerServiceAndCached(t *testing.T) {
	var calls atomic.Int32

	useTokenSource(t, func(_ context.Context, service string) (string, error) {
		calls.Add(1)

		return fakeJWT(time.Now().Add(time.Hour)) + "-" + service, nil
	})

	a1, err := common.ServiceToken(context.Background(), common.ServiceChat)
	require.NoError(t, err)

	a2, err := common.ServiceToken(context.Background(), common.ServiceChat)
	require.NoError(t, err)

	b, err := common.ServiceToken(context.Background(), common.ServiceSimilarity)
	require.NoError(t, err)

	assert.Equal(t, a1, a2, "the second call for the same service must reuse the token")
	assert.NotEqual(t, a1, b, "a token issued for one service must not be reused for another")
	assert.EqualValues(t, 2, calls.Load(), "one issue per service")
}

func TestServiceToken_RefreshesNearExpiry(t *testing.T) {
	var calls atomic.Int32

	useTokenSource(t, func(_ context.Context, _ string) (string, error) {
		calls.Add(1)

		// 10 s left is inside the 30 s refresh skew.
		return fakeJWT(time.Now().Add(10 * time.Second)), nil
	})

	_, err := common.ServiceToken(context.Background(), common.ServiceChat)
	require.NoError(t, err)

	_, err = common.ServiceToken(context.Background(), common.ServiceChat)
	require.NoError(t, err)

	assert.EqualValues(t, 2, calls.Load(), "a token close to expiry must not be reused")
}

func TestServiceToken_DoesNotCacheTokenWithoutExp(t *testing.T) {
	var calls atomic.Int32

	useTokenSource(t, func(_ context.Context, _ string) (string, error) {
		calls.Add(1)

		return "not-a-jwt", nil
	})

	for range 2 {
		tok, err := common.ServiceToken(context.Background(), common.ServiceChat)
		require.NoError(t, err)
		assert.Equal(t, "not-a-jwt", tok)
	}

	assert.EqualValues(t, 2, calls.Load())
}

func TestServiceToken_SourceErrorIsReturned(t *testing.T) {
	useTokenSource(t, func(_ context.Context, _ string) (string, error) {
		return "", errors.New("no catalog login")
	})

	_, err := common.ServiceToken(context.Background(), common.ServiceChat)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chat")
}

func TestDoRequestFull_SendsServiceTokenOnlyWhenAsked(t *testing.T) {
	var gotAuth atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	useTokenSource(t, func(_ context.Context, service string) (string, error) {
		return "tok-for-" + service, nil
	})

	cfg := common.HTTPConfigForTimeout(5 * time.Second)

	// No Service set: the request carries no Authorization header (translate, extract).
	_, err := common.DoRequestFull(context.Background(), http.MethodGet, srv.URL, nil, "", cfg)
	require.NoError(t, err)
	assert.Equal(t, "", gotAuth.Load())

	// Service set: the header carries that service's token.
	cfg.Service = common.ServiceSummarize
	_, err = common.DoRequestFull(context.Background(), http.MethodPost, srv.URL, bytes.NewBufferString("{}"), "application/json", cfg)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok-for-summarize", gotAuth.Load())
}

func TestDoRequestFull_TokenFailureSendsNoAuthorization(t *testing.T) {
	var gotAuth atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	useTokenSource(t, func(_ context.Context, _ string) (string, error) {
		return "", errors.New("no catalog login")
	})

	cfg := common.HTTPConfigForTimeout(5 * time.Second)
	cfg.Service = common.ServiceDigitize

	resp, err := common.DoRequestFull(context.Background(), http.MethodGet, srv.URL, nil, "", cfg)
	require.NoError(t, err)

	// The request still goes out so the service's own 401 is what the test sees.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "", gotAuth.Load())
}
