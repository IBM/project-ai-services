package deployment

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// remoteProviderFor selects the behavior of a remote provider. It is the only place that branches
// on the provider ID for this: a hosted vLLM is talked to directly, every other provider is
// served through the gateway.
func remoteProviderFor(providerID string) types.RemoteProvider {
	if providerID == catalogconstants.ConnectorProviderHostedVLLM {
		return vllmProvider{}
	}

	return litellmProvider{}
}

// vllmProvider is a hosted vLLM server, reached at the plan's api_base.
type vllmProvider struct{}

// NormalizeParams handles the api_base, which the user may give with or without /v1. planParams
// get the bare server URL, and routeParams get it with /v1, since vLLM serves its OpenAI API
// under /v1 and LiteLLM appends only the endpoint path (e.g. /chat/completions).
func (vllmProvider) NormalizeParams(planParams, routeParams map[string]any) {
	apiBase, _ := planParams[catalogconstants.ParamAPIBase].(string)
	if apiBase == "" {
		return
	}

	bare := bareAPIBase(apiBase)
	planParams[catalogconstants.ParamAPIBase] = bare
	routeParams[catalogconstants.ParamAPIBase] = bare + openAIAPIPath
}

// RequiresRoute is false: vLLM is queried directly, not through the gateway.
func (vllmProvider) RequiresRoute() bool { return false }

// MaxModelLen reads max_model_len from the vLLM models endpoint of the plan's api_base.
func (vllmProvider) MaxModelLen(ctx context.Context, plan *types.RemotePlan, clients types.RemoteClients) (int, error) {
	apiBase, _ := plan.Req.Params[catalogconstants.ParamAPIBase].(string)
	apiKey, _ := plan.Req.Params[catalogconstants.ParamAPIKey].(string)

	if apiBase == "" {
		return 0, validators.Unprocessable("no api_base to resolve the maximum model length of model %q from", plan.Req.Model)
	}

	modelsResp, err := clients.VLLM.GetModels(ctx, apiBase, apiKey)
	if err != nil {
		return 0, validators.Unprocessable("failed to fetch models from vllm: %v", err)
	}

	for _, m := range modelsResp.Data {
		if m.ID != plan.Req.Model {
			continue
		}

		// An omitted max_model_len decodes to 0, which is not a usable limit.
		if m.MaxModelLen <= 0 {
			return 0, validators.Unprocessable("vllm reported no valid maximum model length for model %q", plan.Req.Model)
		}

		return m.MaxModelLen, nil
	}

	return 0, validators.Unprocessable("could not resolve maximum model length for model %q", plan.Req.Model)
}

// litellmProvider is any provider served through the gateway (e.g. watsonx).
type litellmProvider struct{}

// NormalizeParams leaves the params as given.
func (litellmProvider) NormalizeParams(_, _ map[string]any) {}

// RequiresRoute is true: the model info is looked up by the route's gateway ID.
func (litellmProvider) RequiresRoute() bool { return true }

// MaxModelLen reads max_input_tokens from the LiteLLM model info of the registered route. As for
// vLLM, a model whose limit is missing or unusable is a 422; a failure of the gateway itself, or a
// route that was never registered, is an internal error.
func (litellmProvider) MaxModelLen(ctx context.Context, plan *types.RemotePlan, clients types.RemoteClients) (int, error) {
	litellmModelID := plan.Route.LiteLLMModelID
	if litellmModelID == "" {
		return 0, fmt.Errorf("litellm model ID is empty")
	}

	infoResp, err := clients.Gateway.GetModelInfo(ctx, litellmModelID)
	if err != nil {
		return 0, fmt.Errorf("failed to get model info from litellm: %w", err)
	}

	for _, d := range infoResp.Data {
		if d.ModelInfo.ID != litellmModelID {
			continue
		}

		// An omitted max_input_tokens decodes to 0, which is not a usable limit.
		if d.ModelInfo.MaxInputTokens <= 0 {
			return 0, validators.Unprocessable("litellm model info reports no valid maximum input tokens for model %q", plan.Req.Model)
		}

		return d.ModelInfo.MaxInputTokens, nil
	}

	return 0, validators.Unprocessable("could not resolve maximum input tokens from litellm model info for model %q", plan.Req.Model)
}
