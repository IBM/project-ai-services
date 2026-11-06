package litellm

import (
	"context"
	"fmt"
	"maps"

	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Keys of litellm_params that the platform owns; a provider param can never override them.
const (
	litellmParamModel = "model"
	litellmParamTags  = "tags"
)

func modelRequest(r Route) catalogclient.LiteLLMModelNewRequest {
	params := make(map[string]any, len(r.Params))
	maps.Copy(params, r.Params)

	if r.APIBase != "" {
		params[ParamAPIBase] = r.APIBase
	}

	params[litellmParamModel] = r.UpstreamModel
	params[litellmParamTags] = []string{r.Tag}

	req := catalogclient.LiteLLMModelNewRequest{
		ModelName:     r.Model,
		LiteLLMParams: params,
	}

	if r.Mode != "" {
		req.ModelInfo = &catalogclient.LiteLLMModelInfo{Mode: r.Mode}
	}

	return req
}

func passThroughID(r Route) string {
	return fmt.Sprintf("%s-token-passthrough-%s", r.Provider, r.Tag)
}

func passThroughRequest(r Route) catalogclient.LiteLLMPassThroughRequest {
	return catalogclient.LiteLLMPassThroughRequest{
		ID:      passThroughID(r),
		Path:    fmt.Sprintf("/tokenize/%s", r.Tag),
		Target:  r.TokenizeURL,
		Methods: []string{"POST"},
		Auth:    true,
	}
}

// ProbeError reports that a route was registered but the gateway could not reach the model.
type ProbeError struct {
	reason string
}

func (e *ProbeError) Error() string {
	return "LiteLLM connectivity probe failed: " + e.reason
}

// Gateway wraps the LiteLLM client with the registration workflow shared by all model deployment types.
type Gateway struct {
	client catalogclient.LiteLLMClient
}

// NewGateway creates a Gateway that registers routes through client.
func NewGateway(client catalogclient.LiteLLMClient) Gateway {
	return Gateway{client: client}
}

// Register adds the route to LiteLLM and verifies it with a health probe. If the probe fails the
// route is removed again and a *ProbeError is returned. A failure to register the tokenize
// passthrough is also an error and triggers the same rollback.
func (g Gateway) Register(ctx context.Context, r *Route) error {
	modelID, err := g.client.RegisterModel(ctx, modelRequest(*r))
	if err != nil {
		return fmt.Errorf("failed to register model in litellm: %w", err)
	}
	r.LiteLLMModelID = modelID

	if err := g.probe(ctx, r.LiteLLMModelID); err != nil {
		g.Deregister(ctx, *r)

		return err
	}

	if r.TokenizeURL != "" {
		if err := g.client.RegisterPassThrough(ctx, passThroughRequest(*r)); err != nil {
			g.Deregister(ctx, *r)

			return fmt.Errorf("failed to register tokenize passthrough for %s: %w", r.Tag, err)
		}
	}

	return nil
}

// Deregister removes the route (best effort). It runs on a context detached from cancellation so
// rollback still happens when the originating request has been cancelled.
func (g Gateway) Deregister(ctx context.Context, r Route) {
	ctx = context.WithoutCancel(ctx)

	if r.LiteLLMModelID != "" {
		if err := g.client.DeregisterModel(ctx, r.LiteLLMModelID); err != nil {
			logger.WarningfCtx(ctx, "failed to deregister litellm model %q: %v", r.LiteLLMModelID, err)
		}
	}

	if r.TokenizeURL != "" {
		if err := g.client.DeregisterPassThrough(ctx, passThroughID(r)); err != nil {
			logger.WarningfCtx(ctx, "failed to deregister tokenize passthrough %q: %v", passThroughID(r), err)
		}
	}
}

func (g Gateway) probe(ctx context.Context, litellmModelID string) error {
	resp, err := g.client.HealthProbe(ctx, litellmModelID)
	if err != nil {
		return &ProbeError{reason: err.Error()}
	}

	if resp != nil && resp.HealthyCount > 0 {
		return nil
	}

	return &ProbeError{reason: "endpoint is unhealthy"}
}
