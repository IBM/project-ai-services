package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVLLMClient_GetModels(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		status     int
		body       string
		pathSuffix string // appended to the server URL to form apiBase
		want       *VLLMModelsResponse
		wantErrMsg string
	}{
		{
			name:   "lists the models",
			status: http.StatusOK,
			body:   `{"data":[{"id":"a","max_model_len":4096},{"id":"b"}]}`,
			want: &VLLMModelsResponse{Data: []VLLMModelInfo{
				{ID: "a", MaxModelLen: 4096},
				{ID: "b"},
			}},
		},
		{
			name:   "sends the api key as a bearer token",
			apiKey: "secret",
			status: http.StatusOK,
			body:   `{"data":[]}`,
			want:   &VLLMModelsResponse{Data: []VLLMModelInfo{}},
		},
		{
			name:       "a trailing slash on api_base is ignored",
			status:     http.StatusOK,
			body:       `{"data":[]}`,
			pathSuffix: "/",
			want:       &VLLMModelsResponse{Data: []VLLMModelInfo{}},
		},
		{
			name:       "non-200 status",
			status:     http.StatusUnauthorized,
			body:       `unauthorized`,
			wantErrMsg: "vllm returned status code 401: unauthorized",
		},
		{
			name:       "invalid json",
			status:     http.StatusOK,
			body:       `not json`,
			wantErrMsg: "failed to unmarshal vllm response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth, gotPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			got, err := NewVLLMClient().GetModels(context.Background(), server.URL+tt.pathSuffix, tt.apiKey)

			assert.Equal(t, "/v1/models", gotPath)

			if tt.apiKey != "" {
				assert.Equal(t, "Bearer "+tt.apiKey, gotAuth)
			} else {
				assert.Empty(t, gotAuth, "no key, no Authorization header")
			}

			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVLLMClient_GetModels_Unreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // nothing listens at url any more

	got, err := NewVLLMClient().GetModels(context.Background(), url, "")

	require.ErrorContains(t, err, "failed to get models from vllm")
	assert.Nil(t, got)
}
