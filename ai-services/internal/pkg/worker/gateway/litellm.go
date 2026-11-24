package gateway

import (
	"context"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerroutes "github.com/project-ai-services/ai-services/internal/pkg/worker/routes"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// proxyFor returns the ProxyManager that sends route commands to the named worker.
// It is a field-free hook so tests can replace it.
var proxyFor = func(g *Gateway, workerName string) proxy.ProxyManager {
	return proxy.NewRemoteProxyManager(stream.New(workerName, g.registry))
}

// liteLLMEgressEnabled reports whether a worker needs the LiteLLM egress route. Only a remote
// Podman worker does: the local worker shares the control plane's network, and OpenShift workers
// do not run Caddy.
func (g *Gateway) liteLLMEgressEnabled(workerName string) bool {
	if g.runtimeType != types.RuntimeTypePodman {
		return false
	}

	if strings.EqualFold(workerName, workerconstants.LocalWorkerName) {
		return false
	}

	rt, ok := g.registry.WorkerRuntimeType(workerName)

	return ok && rt == types.RuntimeTypePodman.String()
}

// registerLiteLLMEgress registers the LiteLLM egress route on the worker's Caddy so services on
// the worker can reach LiteLLM through the control plane. If registration fails, it forwards the
// error to errCh so CommandStream can terminate and report the failure.
func (g *Gateway) registerLiteLLMEgress(ctx context.Context, workerName string, errCh chan<- error) {
	if !g.liteLLMEgressEnabled(workerName) {
		return
	}

	pm := proxyFor(g, workerName)
	if err := workerroutes.RegisterLiteLLMEgress(ctx, pm, workerName); err != nil {
		if ctx.Err() == nil {
			logger.ErrorfCtx(ctx, "WorkerGateway: %v", err)
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
		}
	}
}
