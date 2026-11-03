package deployment

import (
	"context"
	"fmt"
	"maps"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Keys of litellm_params that the platform owns; a provider param can never override them.
const (
	litellmParamModel = "model"
	litellmParamTags  = "tags"
)

func modelRequest(r types.Route) catalogclient.LiteLLMModelNewRequest {
	params := make(map[string]any, len(r.Params))
	maps.Copy(params, r.Params)

	if r.APIBase != "" {
		params[paramAPIBase] = r.APIBase
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

func passThroughID(r types.Route) string {
	return fmt.Sprintf("%s-token-passthrough-%s", r.Provider, r.Tag)
}

func passThroughRequest(r types.Route) catalogclient.LiteLLMPassThroughRequest {
	return catalogclient.LiteLLMPassThroughRequest{
		ID:      passThroughID(r),
		Path:    fmt.Sprintf("/tokenize/%s", r.Tag),
		Target:  r.TokenizeURL,
		Methods: []string{"POST"},
		Auth:    true,
	}
}

// probeError reports that a route was registered but the gateway could not reach the model.
type probeError struct {
	reason string
}

func (e *probeError) Error() string {
	return "LiteLLM connectivity probe failed: " + e.reason
}

// gateway wraps the LiteLLM client with the registration workflow shared by all deployment types.
type gateway struct {
	client catalogclient.LiteLLMClient
}

// register adds the route to LiteLLM and verifies it with a health probe. If the probe fails the
// route is removed again and a *probeError is returned. A failure to register the tokenize
// passthrough is also an error and triggers the same rollback.
func (g gateway) register(ctx context.Context, r types.Route) error {
	if err := g.client.RegisterModel(ctx, modelRequest(r)); err != nil {
		return fmt.Errorf("failed to register model in litellm: %w", err)
	}

	if err := g.probe(ctx, r.Model); err != nil {
		g.deregister(ctx, r)

		return err
	}

	if r.TokenizeURL != "" {
		if err := g.client.RegisterPassThrough(ctx, passThroughRequest(r)); err != nil {
			g.deregister(ctx, r)

			return fmt.Errorf("failed to register tokenize passthrough for %s: %w", r.Tag, err)
		}
	}

	return nil
}

// deregister removes the route (best effort). It runs on a context detached from cancellation so
// rollback still happens when the originating request has been cancelled.
func (g gateway) deregister(ctx context.Context, r types.Route) {
	ctx = context.WithoutCancel(ctx)

	if err := g.client.DeregisterModel(ctx, r.Model); err != nil {
		logger.WarningfCtx(ctx, "failed to deregister litellm model %q: %v", r.Model, err)
	}

	if r.TokenizeURL != "" {
		if err := g.client.DeregisterPassThrough(ctx, passThroughID(r)); err != nil {
			logger.WarningfCtx(ctx, "failed to deregister tokenize passthrough %q: %v", passThroughID(r), err)
		}
	}
}

func (g gateway) probe(ctx context.Context, model string) error {
	resp, err := g.client.HealthProbe(ctx, model)
	if err != nil {
		return &probeError{reason: err.Error()}
	}

	if resp != nil && resp.HealthyCount > 0 {
		return nil
	}

	reason := "no healthy endpoints"
	if resp != nil && len(resp.UnhealthyEndpoints) > 0 && resp.UnhealthyEndpoints[0].Error != "" {
		reason = resp.UnhealthyEndpoints[0].Error
	}

	return &probeError{reason: reason}
}
