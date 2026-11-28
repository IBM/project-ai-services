package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerroutes "github.com/project-ai-services/ai-services/internal/pkg/worker/routes"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// proxyFor returns the ProxyManager that sends route commands to the named worker.
// It is a field-free hook so tests can replace it.
var proxyFor = func(g *Gateway, workerName string) proxy.ProxyManager {
	return proxy.NewRemoteProxyManager(stream.New(workerName, g.registry))
}

// liteLLMIngressAddr returns the host and port a worker dials to reach the control-plane private
// ingress (LiteLLM). The host is also verified against the gateway server certificate, so it is
// always one of that certificate's SANs (see serverCertDNSNames). It is a field-free hook so tests
// can replace it.
var liteLLMIngressAddr = func(ctx context.Context, runtimeType types.RuntimeType) (host, port string, err error) {
	switch runtimeType {
	case types.RuntimeTypeOpenShift:
		// The control-plane Caddy is reached through its passthrough Route, on the router's port.
		host, err = catalogutils.CaddyRouteHost(ctx)
		if err != nil {
			return "", "", fmt.Errorf("resolve control-plane Caddy route host: %w", err)
		}

		return host, strconv.Itoa(workerconstants.OpenShiftRoutePort), nil
	case types.RuntimeTypePodman:
		suffix := utils.GetEnv(proxy.DomainSuffixEnvVar, "")
		if suffix == "" {
			return "", "", fmt.Errorf("%s environment variable not set", proxy.DomainSuffixEnvVar)
		}

		return workerconstants.WorkerGatewayName + "." + suffix,
			utils.GetEnv(proxy.CaddyPrivateIngressPortEnvVar, proxy.DefaultPrivateIngressPort), nil
	default:
		return "", "", fmt.Errorf("unsupported control-plane runtime type %q", runtimeType)
	}
}

// liteLLMEgressEnabled reports whether a worker needs the LiteLLM egress route: every remote worker
// that runs a Caddy, whatever the runtime of the worker or of the control plane. The co-located
// local worker shares the control plane's network, and an OpenShift worker without a Caddy Route
// has nowhere to register it.
func (g *Gateway) liteLLMEgressEnabled(workerName string) bool {
	if strings.EqualFold(workerName, workerconstants.LocalWorkerName) {
		return false
	}

	rt, ok := g.registry.WorkerRuntimeType(workerName)
	if !ok {
		return false
	}

	switch rt {
	case types.RuntimeTypePodman.String():
		return true
	case types.RuntimeTypeOpenShift.String():
		meta, _ := g.registry.WorkerMetadata(workerName)

		return meta[workerconstants.MetaKeyPrivateIngressHost] != ""
	default:
		return false
	}
}

// registerLiteLLMEgress registers the LiteLLM egress route on the worker's Caddy so services on
// the worker can reach LiteLLM through the control plane. If registration fails, it forwards the
// error to errCh so CommandStream can terminate and report the failure.
func (g *Gateway) registerLiteLLMEgress(ctx context.Context, workerName string, errCh chan<- error) {
	if !g.liteLLMEgressEnabled(workerName) {
		return
	}

	host, port, err := liteLLMIngressAddr(ctx, g.runtimeType)
	if err == nil {
		err = workerroutes.RegisterLiteLLMEgress(ctx, proxyFor(g, workerName), workerName, host, port)
	}

	if err != nil && ctx.Err() == nil {
		err = fmt.Errorf("LiteLLM egress for worker %s: %w", workerName, err)
		logger.ErrorfCtx(ctx, "WorkerGateway: %v", err)

		select {
		case errCh <- err:
		case <-ctx.Done():
		}
	}
}
