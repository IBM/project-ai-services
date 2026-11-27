package deployment

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

const testLimitsModel = "ibm/granite"

// fakeLimitsLiteLLM is a LiteLLM client that records the order of calls and serves a fixed model info.
type fakeLimitsLiteLLM struct {
	catalogclient.LiteLLMClient

	calls []string
	// info is returned by GetModelInfo.
	info    *catalogclient.LiteLLMModelInfoResponse
	infoErr error
}

func (f *fakeLimitsLiteLLM) RegisterModel(_ context.Context, _ catalogclient.LiteLLMModelNewRequest) (string, error) {
	f.calls = append(f.calls, "register")

	return "litellm-id", nil
}

func (f *fakeLimitsLiteLLM) DeregisterModel(_ context.Context, _ string) error {
	f.calls = append(f.calls, "deregister")

	return nil
}

func (f *fakeLimitsLiteLLM) RegisterPassThrough(_ context.Context, _ catalogclient.LiteLLMPassThroughRequest) error {
	f.calls = append(f.calls, "register-passthrough")

	return nil
}

func (f *fakeLimitsLiteLLM) DeregisterPassThrough(_ context.Context, _ string) error {
	f.calls = append(f.calls, "deregister-passthrough")

	return nil
}

func (f *fakeLimitsLiteLLM) HealthProbe(_ context.Context, _ string) (*catalogclient.LiteLLMHealthResponse, error) {
	return &catalogclient.LiteLLMHealthResponse{HealthyCount: 1}, nil
}

func (f *fakeLimitsLiteLLM) GetModelInfo(_ context.Context, _ string) (*catalogclient.LiteLLMModelInfoResponse, error) {
	f.calls = append(f.calls, "model-info")

	return f.info, f.infoErr
}

// fakeConnectorRepo stores the inserted connector; every other method is unused.
type fakeConnectorRepo struct {
	dbrepo.ConnectorRepository

	inserted []*dbmodels.Connector
}

func (r *fakeConnectorRepo) Insert(_ context.Context, c *dbmodels.Connector) error {
	r.inserted = append(r.inserted, c)

	return nil
}

// fakeVLLM is a vLLM client that serves a fixed response and records what it was asked.
type fakeVLLM struct {
	resp *catalogclient.VLLMModelsResponse
	err  error

	calls           int
	apiBase, apiKey string
}

func (f *fakeVLLM) GetModels(_ context.Context, apiBase, apiKey string) (*catalogclient.VLLMModelsResponse, error) {
	f.calls++
	f.apiBase, f.apiKey = apiBase, apiKey

	return f.resp, f.err
}

func vllmModels(models ...catalogclient.VLLMModelInfo) *catalogclient.VLLMModelsResponse {
	return &catalogclient.VLLMModelsResponse{Data: models}
}

func modelInfo(id string, maxInputTokens int) *catalogclient.LiteLLMModelInfoResponse {
	return &catalogclient.LiteLLMModelInfoResponse{
		Data: []catalogclient.LiteLLMModelData{
			{ModelName: testLimitsModel, ModelInfo: catalogclient.LiteLLMInfoRec{ID: id, MaxInputTokens: maxInputTokens}},
		},
	}
}

func remotePlan(provider string, params map[string]any) *types.RemotePlan {
	return &types.RemotePlan{
		Req:      apimodels.CreateModelRequest{ProviderID: provider, Model: testLimitsModel, Params: params},
		Provider: remoteProviderFor(provider),
	}
}

// requireUnprocessable asserts err is a 422 validation error whose message contains msg.
func requireUnprocessable(t *testing.T, err error, msg string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, http.StatusUnprocessableEntity, valErr.Code)
	assert.Contains(t, valErr.Message, msg)
}

func TestRemoteProviderFor(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		wantType      types.RemoteProvider
		wantNeedRoute bool
	}{
		{"hosted vllm is asked directly, before registration", catalogconstants.ConnectorProviderHostedVLLM, vllmProvider{}, false},
		{"watsonx goes through the gateway, after registration", catalogconstants.ConnectorProviderWatsonx, litellmProvider{}, true},
		{"any other provider goes through the gateway", "openai", litellmProvider{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteProviderFor(tt.provider)

			assert.IsType(t, tt.wantType, got)
			assert.Equal(t, tt.wantNeedRoute, got.RequiresRoute())
		})
	}
}

// NormalizeParams is where the vLLM api_base is made consistent: the plan keeps the bare server
// URL and the gateway route gets it with /v1. Other providers are left untouched.
func TestRemoteProvider_NormalizeParams(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		planParams map[string]any
		wantPlan   map[string]any
		wantRoute  map[string]any
	}{
		{
			name:       "vllm: bare api_base gets /v1 on the route",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			planParams: map[string]any{"api_base": "http://h:8000"},
			wantPlan:   map[string]any{"api_base": "http://h:8000"},
			wantRoute:  map[string]any{"api_base": "http://h:8000/v1"},
		},
		{
			name:       "vllm: /v1 and trailing slashes are stripped from the plan",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			planParams: map[string]any{"api_base": "http://h:8000/v1/"},
			wantPlan:   map[string]any{"api_base": "http://h:8000"},
			wantRoute:  map[string]any{"api_base": "http://h:8000/v1"},
		},
		{
			name:       "vllm: other params are left alone",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			planParams: map[string]any{"api_base": "http://h:8000", "api_key": "k"},
			wantPlan:   map[string]any{"api_base": "http://h:8000", "api_key": "k"},
			wantRoute:  map[string]any{"api_base": "http://h:8000/v1", "api_key": "k"},
		},
		{
			name:       "vllm: no api_base changes nothing",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			planParams: map[string]any{},
			wantPlan:   map[string]any{},
			wantRoute:  map[string]any{},
		},
		{
			name:       "watsonx: api_base is forwarded as given",
			provider:   catalogconstants.ConnectorProviderWatsonx,
			planParams: map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com/v1/"},
			wantPlan:   map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com/v1/"},
			wantRoute:  map[string]any{"api_base": "https://us-south.ml.cloud.ibm.com/v1/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routeParams := maps.Clone(tt.planParams)

			remoteProviderFor(tt.provider).NormalizeParams(tt.planParams, routeParams)

			assert.Equal(t, tt.wantPlan, tt.planParams)
			assert.Equal(t, tt.wantRoute, routeParams)
		})
	}
}

func TestVLLMProvider_MaxModelLen(t *testing.T) {
	granite := func(maxLen int) catalogclient.VLLMModelInfo {
		return catalogclient.VLLMModelInfo{ID: testLimitsModel, MaxModelLen: maxLen}
	}

	tests := []struct {
		name       string
		resp       *catalogclient.VLLMModelsResponse
		clientErr  error
		params     map[string]any
		want       int
		wantErrMsg string // every failure is a 422
		wantCalled bool   // whether vLLM is asked at all
	}{
		{
			name:       "model found among several",
			resp:       vllmModels(catalogclient.VLLMModelInfo{ID: "other", MaxModelLen: 1}, granite(4096)),
			params:     map[string]any{"api_base": "http://h:8000"},
			want:       4096,
			wantCalled: true,
		},
		{
			name:       "api_base and api_key are passed to the client",
			resp:       vllmModels(granite(8192)),
			params:     map[string]any{"api_base": "http://h:8000", "api_key": "secret"},
			want:       8192,
			wantCalled: true,
		},
		{
			name:       "max_model_len omitted",
			resp:       vllmModels(granite(0)),
			params:     map[string]any{"api_base": "http://h:8000"},
			wantErrMsg: "no valid maximum model length",
			wantCalled: true,
		},
		{
			name:       "max_model_len negative",
			resp:       vllmModels(granite(-1)),
			params:     map[string]any{"api_base": "http://h:8000"},
			wantErrMsg: "no valid maximum model length",
			wantCalled: true,
		},
		{
			name:       "model not listed",
			resp:       vllmModels(catalogclient.VLLMModelInfo{ID: "other", MaxModelLen: 4096}),
			params:     map[string]any{"api_base": "http://h:8000"},
			wantErrMsg: "could not resolve maximum model length",
			wantCalled: true,
		},
		{
			name:       "no models at all",
			resp:       vllmModels(),
			params:     map[string]any{"api_base": "http://h:8000"},
			wantErrMsg: "could not resolve maximum model length",
			wantCalled: true,
		},
		{
			name:       "client error is a 422 that names vllm",
			clientErr:  errors.New("connection refused"),
			params:     map[string]any{"api_base": "http://h:8000"},
			wantErrMsg: "failed to fetch models from vllm: connection refused",
			wantCalled: true,
		},
		{
			name:       "no api_base never reaches the client",
			params:     map[string]any{},
			wantErrMsg: "no api_base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeVLLM{resp: tt.resp, err: tt.clientErr}

			got, err := vllmProvider{}.MaxModelLen(context.Background(), remotePlan("hosted_vllm", tt.params), types.RemoteClients{VLLM: client})

			if tt.wantCalled {
				assert.Equal(t, 1, client.calls)
				assert.Equal(t, tt.params["api_base"], client.apiBase)

				wantKey, _ := tt.params["api_key"].(string)
				assert.Equal(t, wantKey, client.apiKey)
			} else {
				assert.Zero(t, client.calls, "vllm must not be asked without an api_base")
			}

			if tt.wantErrMsg != "" {
				requireUnprocessable(t, err, tt.wantErrMsg)
				assert.Zero(t, got, "a failed lookup must not return a usable value")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLiteLLMProvider_MaxModelLen(t *testing.T) {
	tests := []struct {
		name       string
		modelID    string // plan.Route.LiteLLMModelID
		info       *catalogclient.LiteLLMModelInfoResponse
		infoErr    error
		want       int
		wantErrMsg string
		// wantUnprocessable: the model's limit is unusable, which is a 422 like for vLLM. Any other
		// failure is the gateway's or ours, and stays an internal error.
		wantUnprocessable bool
		wantCalled        bool // whether the gateway is asked at all
	}{
		{
			name:       "model found",
			modelID:    "litellm-id",
			info:       modelInfo("litellm-id", 131072),
			want:       131072,
			wantCalled: true,
		},
		{
			name:    "picks the entry of the registered route",
			modelID: "litellm-id",
			info: &catalogclient.LiteLLMModelInfoResponse{Data: []catalogclient.LiteLLMModelData{
				{ModelInfo: catalogclient.LiteLLMInfoRec{ID: "someone-else", MaxInputTokens: 1}},
				{ModelInfo: catalogclient.LiteLLMInfoRec{ID: "litellm-id", MaxInputTokens: 2048}},
			}},
			want:       2048,
			wantCalled: true,
		},
		{
			name:              "max_input_tokens omitted",
			modelID:           "litellm-id",
			info:              modelInfo("litellm-id", 0),
			wantErrMsg:        "no valid maximum input tokens",
			wantUnprocessable: true,
			wantCalled:        true,
		},
		{
			name:              "max_input_tokens negative",
			modelID:           "litellm-id",
			info:              modelInfo("litellm-id", -1),
			wantErrMsg:        "no valid maximum input tokens",
			wantUnprocessable: true,
			wantCalled:        true,
		},
		{
			name:              "route not in the model info",
			modelID:           "litellm-id",
			info:              modelInfo("someone-else", 4096),
			wantErrMsg:        "could not resolve maximum input tokens",
			wantUnprocessable: true,
			wantCalled:        true,
		},
		{
			name:              "empty model info",
			modelID:           "litellm-id",
			info:              &catalogclient.LiteLLMModelInfoResponse{},
			wantErrMsg:        "could not resolve maximum input tokens",
			wantUnprocessable: true,
			wantCalled:        true,
		},
		{
			name:       "gateway error",
			modelID:    "litellm-id",
			infoErr:    errors.New("connection refused"),
			wantErrMsg: "failed to get model info from litellm: connection refused",
			wantCalled: true,
		},
		{
			name:       "route not registered yet",
			wantErrMsg: "litellm model ID is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeLimitsLiteLLM{info: tt.info, infoErr: tt.infoErr}
			plan := remotePlan("watsonx", map[string]any{})
			plan.Route.LiteLLMModelID = tt.modelID

			got, err := litellmProvider{}.MaxModelLen(context.Background(), plan, types.RemoteClients{Gateway: litellm.NewGateway(client)})

			if tt.wantCalled {
				assert.Equal(t, []string{"model-info"}, client.calls)
			} else {
				assert.Empty(t, client.calls, "no gateway call without a registered route")
			}

			if tt.wantErrMsg != "" {
				assert.Zero(t, got, "a failed lookup must not return a usable value")

				if tt.wantUnprocessable {
					requireUnprocessable(t, err, tt.wantErrMsg)

					return
				}

				require.ErrorContains(t, err, tt.wantErrMsg)

				var valErr *validators.ValidationError
				assert.False(t, errors.As(err, &valErr), "a gateway failure must not look like a client error")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ExecuteRemote resolves the limit before registering the route when it can (vLLM), and after
// registering it when it must (LiteLLM), rolling the route back on a failure.
func TestExecuteRemote_ModelLimitsOrdering(t *testing.T) {
	granite := func(maxLen int) *catalogclient.VLLMModelsResponse {
		return vllmModels(catalogclient.VLLMModelInfo{ID: testLimitsModel, MaxModelLen: maxLen})
	}

	tests := []struct {
		name         string
		provider     string
		vllm         *fakeVLLM
		info         *catalogclient.LiteLLMModelInfoResponse
		wantErrMsg   string
		wantCalls    []string
		wantMaxLen   int
		wantInserted int
	}{
		{
			name:         "vllm is resolved before the route is registered",
			provider:     catalogconstants.ConnectorProviderHostedVLLM,
			vllm:         &fakeVLLM{resp: granite(4096)},
			wantCalls:    []string{"register"},
			wantMaxLen:   4096,
			wantInserted: 1,
		},
		{
			name:       "vllm failure registers nothing",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			vllm:       &fakeVLLM{resp: granite(0)},
			wantErrMsg: "no valid maximum model length",
			wantCalls:  nil,
		},
		{
			name:       "vllm unreachable registers nothing",
			provider:   catalogconstants.ConnectorProviderHostedVLLM,
			vllm:       &fakeVLLM{err: errors.New("connection refused")},
			wantErrMsg: "failed to fetch models from vllm",
			wantCalls:  nil,
		},
		{
			name:         "litellm is resolved after the route is registered",
			provider:     catalogconstants.ConnectorProviderWatsonx,
			vllm:         &fakeVLLM{},
			info:         modelInfo("litellm-id", 131072),
			wantCalls:    []string{"register", "model-info"},
			wantMaxLen:   131072,
			wantInserted: 1,
		},
		{
			name:       "litellm failure rolls the route back",
			provider:   catalogconstants.ConnectorProviderWatsonx,
			vllm:       &fakeVLLM{},
			info:       modelInfo("litellm-id", 0),
			wantErrMsg: "no valid maximum input tokens",
			wantCalls:  []string{"register", "model-info", "deregister"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeLimitsLiteLLM{info: tt.info}
			conns := &fakeConnectorRepo{}
			e := NewModelExecutor(nil, conns, client, tt.vllm)

			plan := remotePlan(tt.provider, map[string]any{catalogconstants.ParamAPIBase: "http://h:8000"})

			err := e.ExecuteRemote(context.Background(), plan)

			assert.Equal(t, tt.wantCalls, client.calls)
			assert.Len(t, conns.inserted, tt.wantInserted)

			if tt.provider == catalogconstants.ConnectorProviderWatsonx {
				assert.Zero(t, tt.vllm.calls, "a gateway provider must not be asked directly")
			}

			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)

				return
			}

			require.NoError(t, err)

			stored, ok := plan.Metadata[catalogconstants.MetadataKeyParams].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantMaxLen, stored[catalogconstants.ParamMaxModelLen])
		})
	}
}
