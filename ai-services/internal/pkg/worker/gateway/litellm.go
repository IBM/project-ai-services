package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

const (
	// liteLLMEgressAttempts and liteLLMEgressRetryInterval bound how long the gateway waits for a
	// joining worker's Caddy, which only starts once the worker has registered.
	liteLLMEgressAttempts      = 30
	liteLLMEgressRetryInterval = 10 * time.Second
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

// liteLLMEgressRoute builds the worker-side route that forwards LiteLLM calls over mTLS to the
// control-plane private ingress. The control plane is dialed at the gateway hostname, which the
// worker already resolves and which is in the gateway server certificate's SANs.
func liteLLMEgressRoute() (proxy.EgressRoute, error) {
	suffix := utils.GetEnv(proxy.DomainSuffixEnvVar, "")
	if suffix == "" {
		return proxy.EgressRoute{}, fmt.Errorf("%s environment variable not set", proxy.DomainSuffixEnvVar)
	}

	host := workerconstants.WorkerGatewayName + "." + suffix
	port := utils.GetEnv(proxy.CaddyPrivateIngressPortEnvVar, proxy.DefaultPrivateIngressPort)

	return proxy.EgressRoute{
		ID:                     workerconstants.LiteLLMEgressRouteID,
		PathPrefix:             workerconstants.LiteLLMPathPrefix,
		DialUpstream:           host + ":" + port,
		ClientCertPath:         workerconstants.WorkerTLSDir + "/tls.crt",
		EncryptedClientKeyPath: workerconstants.WorkerTLSDir + "/tls.key",
		SecretFilePath:         workerconstants.WorkerMTLSKeyPath,
		TrustedCACertPath:      workerconstants.WorkerTLSDir + "/ca.crt",
		ServerName:             host,
	}, nil
}

// registerLiteLLMEgress registers the LiteLLM egress route on the worker's Caddy so services on
// the worker can reach LiteLLM through the control plane. Registering an existing route is a
// no-op, so it is safe on every (re)connect. It retries until the worker's Caddy answers or ctx
// ends (the stream closed).
func (g *Gateway) registerLiteLLMEgress(ctx context.Context, workerName string) {
	if !g.liteLLMEgressEnabled(workerName) {
		return
	}

	route, err := liteLLMEgressRoute()
	if err != nil {
		logger.ErrorfCtx(ctx, "WorkerGateway: cannot register LiteLLM egress on worker %s: %v", workerName, err)

		return
	}

	pm := proxyFor(g, workerName)

	for attempt := 1; attempt <= liteLLMEgressAttempts; attempt++ {
		err = pm.RegisterEgressRoute(ctx, route)
		if err == nil {
			logger.InfofCtx(ctx, "WorkerGateway: registered LiteLLM egress route on worker %s", workerName)

			return
		}

		logger.WarningfCtx(ctx, "WorkerGateway: LiteLLM egress on worker %s (attempt %d/%d): %v",
			workerName, attempt, liteLLMEgressAttempts, err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(liteLLMEgressRetryInterval):
		}
	}

	logger.ErrorfCtx(ctx, "WorkerGateway: gave up registering LiteLLM egress on worker %s: %v", workerName, err)
}
