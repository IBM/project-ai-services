package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestNewLiteLLMClient(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		os.Unsetenv("LITELLM_ENDPOINT")
		os.Unsetenv("LITELLM_MASTER_KEY")
		client := NewLiteLLMClient()
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("with environment variables", func(t *testing.T) {
		t.Setenv("LITELLM_ENDPOINT", "http://litellm.custom:8000/")
		t.Setenv("LITELLM_MASTER_KEY", "sk-custom-master-key")
		client := NewLiteLLMClient()
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		lc, ok := client.(*litellmClient)
		if !ok {
			t.Fatal("expected *litellmClient type")
		}
		if lc.httpClient.BaseURL != "http://litellm.custom:8000" {
			t.Errorf("expected BaseURL http://litellm.custom:8000, got %s", lc.httpClient.BaseURL)
		}
	})
}

func TestRegisterModel(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantModelID  string
		wantErr      bool
	}{
		{
			name:         "success 200 with model_id",
			statusCode:   http.StatusOK,
			responseBody: `{"model_id":"model-12345"}`,
			wantModelID:  "model-12345",
			wantErr:      false,
		},
		{
			name:         "empty model_id in response",
			statusCode:   http.StatusOK,
			responseBody: `{"model_id":""}`,
			wantErr:      true,
		},
		{
			name:         "invalid json",
			statusCode:   http.StatusOK,
			responseBody: `invalid-json`,
			wantErr:      true,
		},
		{
			name:         "server error 500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"failed to create model"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/model/new" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			modelID, err := c.RegisterModel(context.Background(), LiteLLMModelNewRequest{
				ModelName: "gpt-4",
				LiteLLMParams: map[string]any{
					"model": "openai/gpt-4",
				},
				ModelInfo: &LiteLLMModelInfo{
					Mode: "embedding",
				},
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("RegisterModel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if modelID != tt.wantModelID {
				t.Errorf("RegisterModel() modelID = %v, want %v", modelID, tt.wantModelID)
			}
		})
	}
}

func TestDeregisterModel(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantErr      bool
	}{
		{
			name:         "success 200",
			statusCode:   http.StatusOK,
			responseBody: `{"status":"success"}`,
			wantErr:      false,
		},
		{
			name:         "not found 404",
			statusCode:   http.StatusNotFound,
			responseBody: `{"error":"Not Found"}`,
			wantErr:      false,
		},
		{
			name:         "not found 400 with error message",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":{"message":"{'error': 'Model with id=6f20f027-c677-40a2-9a28-309bbc227942 not found in db'}","type":"auth_error","param":"None","code":"400"}}`,
			wantErr:      false,
		},
		{
			name:         "bad request 400 with other error",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":{"message":"Invalid parameter"}}`,
			wantErr:      true,
		},
		{
			name:         "server error 500 with not found in body",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"database not found error"}`,
			wantErr:      true,
		},
		{
			name:         "server error 500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"Internal server error"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/model/delete" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			err := c.DeregisterModel(context.Background(), "test-model-id")
			if (err != nil) != tt.wantErr {
				t.Errorf("DeregisterModel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterPassThrough(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantErr      bool
	}{
		{
			name:         "success 200",
			statusCode:   http.StatusOK,
			responseBody: `{"status":"success"}`,
			wantErr:      false,
		},
		{
			name:         "error 500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"failed to create passthrough"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/config/pass_through_endpoint" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			err := c.RegisterPassThrough(context.Background(), LiteLLMPassThroughRequest{
				ID:      "ep-1",
				Path:    "/v1/custom",
				Target:  "http://backend:8080",
				Methods: []string{"GET", "POST"},
				Auth:    true,
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterPassThrough() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeregisterPassThrough(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantErr      bool
	}{
		{
			name:         "success 200",
			statusCode:   http.StatusOK,
			responseBody: `{"status":"success"}`,
			wantErr:      false,
		},
		{
			name:         "not found 404",
			statusCode:   http.StatusNotFound,
			responseBody: `{"error":"Not Found"}`,
			wantErr:      false,
		},
		{
			name:         "not found 400 with message",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"endpoint not found"}`,
			wantErr:      false,
		},
		{
			name:         "not found 400 no pass-through endpoints setup",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"detail":{"error":"There are no pass-through endpoints setup."}}`,
			wantErr:      false,
		},
		{
			name:         "bad request 400 other error",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"invalid endpoint id"}`,
			wantErr:      true,
		},
		{
			name:         "server error 500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"internal error"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/config/pass_through_endpoint" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.URL.Query().Get("endpoint_id") != "test-ep-id" {
					t.Errorf("unexpected endpoint_id query param: %s", r.URL.Query().Get("endpoint_id"))
				}
				if r.Method != http.MethodDelete {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			err := c.DeregisterPassThrough(context.Background(), "test-ep-id")
			if (err != nil) != tt.wantErr {
				t.Errorf("DeregisterPassThrough() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteKeysByAlias(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantErr      bool
	}{
		{
			name:         "success 200",
			statusCode:   http.StatusOK,
			responseBody: `{"status":"success"}`,
			wantErr:      false,
		},
		{
			name:         "not found 404",
			statusCode:   http.StatusNotFound,
			responseBody: `{"error":"Not Found"}`,
			wantErr:      false,
		},
		{
			name:         "not found 400 with message",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"Key alias not found"}`,
			wantErr:      false,
		},
		{
			name:         "bad request 400 other error",
			statusCode:   http.StatusBadRequest,
			responseBody: `{"error":"Invalid request"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/key/delete" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			err := c.DeleteKeysByAlias(context.Background(), []string{"alias1"})
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteKeysByAlias() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		wantKey      string
		wantErr      bool
	}{
		{
			name:         "success 200 with key",
			statusCode:   http.StatusOK,
			responseBody: `{"key":"sk-litellm-123456"}`,
			wantKey:      "sk-litellm-123456",
			wantErr:      false,
		},
		{
			name:         "200 with empty key",
			statusCode:   http.StatusOK,
			responseBody: `{"key":""}`,
			wantErr:      true,
		},
		{
			name:         "invalid json",
			statusCode:   http.StatusOK,
			responseBody: `invalid-json`,
			wantErr:      true,
		},
		{
			name:         "error 500",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"internal error"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/key/generate" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			key, err := c.GenerateKey(context.Background(), LiteLLMKeyGenerateRequest{
				KeyAlias: "test-alias",
				Models:   []string{"model-1"},
				Metadata: LiteLLMKeyMetadata{
					Tags: []string{"tag-1"},
				},
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if key != tt.wantKey {
				t.Errorf("GenerateKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestHealthProbe(t *testing.T) {
	tests := []struct {
		name         string
		modelID      string
		statusCode   int
		responseBody string
		wantErr      bool
	}{
		{
			name:       "success 200",
			modelID:    "model-123",
			statusCode: http.StatusOK,
			responseBody: `{
				"healthy_endpoints": [{"api_base":"http://localhost:8000","model":"model-123","model_id":"id-1"}],
				"unhealthy_endpoints": [],
				"healthy_count": 1,
				"unhealthy_count": 0
			}`,
			wantErr: false,
		},
		{
			name:         "invalid json",
			modelID:      "model-123",
			statusCode:   http.StatusOK,
			responseBody: `invalid-json`,
			wantErr:      true,
		},
		{
			name:         "server error 500",
			modelID:      "model-123",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error":"health probe failed"}`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.URL.Query().Get("model_id") != tt.modelID {
					t.Errorf("unexpected model_id query param: %s", r.URL.Query().Get("model_id"))
				}
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			c := &litellmClient{
				httpClient: resty.New().SetBaseURL(server.URL),
			}

			resp, err := c.HealthProbe(context.Background(), tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("HealthProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (resp == nil || resp.HealthyCount != 1) {
				t.Errorf("HealthProbe() unexpected response: %+v", resp)
			}
		})
	}
}
