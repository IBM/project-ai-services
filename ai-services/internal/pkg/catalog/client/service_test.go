package client

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/httpproxy"
)

type recordingProxy struct {
	headers map[string]string
	url     string
	calls   int
}

func (r *recordingProxy) HTTPProxy(_ context.Context, _, targetURL string, headers map[string]string, _ []byte) (*httpproxy.Response, error) {
	r.calls++
	r.url = targetURL
	r.headers = headers

	return &httpproxy.Response{StatusCode: http.StatusOK, Body: []byte(`{"total":0,"items":[]}`)}, nil
}

type fakeTokens struct {
	token string
	err   error
	asked string
}

func (f *fakeTokens) InternalToken(_ context.Context, service string) (string, error) {
	f.asked = service

	return f.token, f.err
}

func TestServiceClient_SendsBearerTokenOnEveryCall(t *testing.T) {
	proxy := &recordingProxy{}
	tokens := &fakeTokens{token: "tok-123"}
	c := NewServiceClient(proxy, "http://digitize:4000", tokens)

	_, err := c.ListConnectors(context.Background(), 0, 0, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok-123", proxy.headers["Authorization"])
	assert.Equal(t, "digitize", tokens.asked)

	// A call with a body keeps its Content-Type and also carries the token.
	require.NoError(t, c.Disconnect(context.Background(), "abc"))
	assert.Equal(t, "Bearer tok-123", proxy.headers["Authorization"])
	require.NoError(t, c.UpdateConnector(context.Background(), "abc", map[string]any{"k": "v"}))
	assert.Equal(t, "Bearer tok-123", proxy.headers["Authorization"])
	assert.Equal(t, "application/json", proxy.headers["Content-Type"])
}

func TestServiceClient_TokenFailureStopsTheRequest(t *testing.T) {
	proxy := &recordingProxy{}
	c := NewServiceClient(proxy, "http://digitize:4000", &fakeTokens{err: errors.New("signer down")})

	_, err := c.ListConnectors(context.Background(), 0, 0, "")
	require.Error(t, err)
	assert.Equal(t, 0, proxy.calls, "no unauthenticated request may be sent when the token cannot be obtained")
}

func TestServiceClient_NilTokensSendsNoAuthorization(t *testing.T) {
	proxy := &recordingProxy{}
	c := NewServiceClient(proxy, "http://digitize:4000", nil)

	_, err := c.ListConnectors(context.Background(), 0, 0, "")
	require.NoError(t, err)
	assert.NotContains(t, proxy.headers, "Authorization")
}
