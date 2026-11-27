package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestModelClient(t *testing.T, handler http.HandlerFunc) *ModelClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &ModelClient{client: &Client{httpClient: resty.New().SetBaseURL(srv.URL)}}
}

func TestModelClient_ListModels(t *testing.T) {
	var gotQuery map[string][]string
	mc := newTestModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		assert.Equal(t, "/api/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"abc","name":"granite-llm","deployment_type":"local"}],"pagination":{"page":1,"has_next":false}}`))
	})

	resp, err := mc.ListModels(context.Background(), &ListModelsParams{Page: 2, PageSize: 50, Search: "gran"})
	require.NoError(t, err)

	require.Len(t, resp.Data, 1)
	assert.Equal(t, "granite-llm", resp.Data[0].Name)
	assert.Equal(t, []string{"2"}, gotQuery["page"])
	assert.Equal(t, []string{"50"}, gotQuery["page_size"])
	assert.Equal(t, []string{"gran"}, gotQuery["q"])
}

func TestModelClient_GetModel(t *testing.T) {
	mc := newTestModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/models/abc", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc","name":"granite-llm","applications":[{"id":"1","name":"my-app"}]}`))
	})

	detail, err := mc.GetModel(context.Background(), "abc")
	require.NoError(t, err)

	assert.Equal(t, "granite-llm", detail.Name)
	require.Len(t, detail.Applications, 1)
	assert.Equal(t, "my-app", detail.Applications[0].Name)
}

func TestModelClient_ErrorMapping(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound} {
		mc := newTestModelClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		})

		_, err := mc.GetModel(context.Background(), "abc")

		var httpErr *HTTPError
		require.True(t, errors.As(err, &httpErr))
		assert.Equal(t, code, httpErr.StatusCode)
	}
}
