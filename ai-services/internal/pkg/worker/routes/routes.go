// Package routes manages worker-side proxy routes for private mTLS communication with the control plane.
package routes

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	// liteLLMEgressAttempts and liteLLMEgressRetryInterval bound how long the gateway waits for a
	// joining worker's Caddy, which only starts once the worker has registered.
	liteLLMEgressAttempts      = 30
	liteLLMEgressRetryInterval = 10 * time.Second
)

// LiteLLMEgressRoute builds the worker-side route that forwards LiteLLM calls over mTLS to the
// control-plane private ingress, dialed at host:port. host is also the name verified against the
// gateway server certificate, so it has to be one of that certificate's SANs and one the worker's
// Caddy can resolve.
func LiteLLMEgressRoute(host, port string) proxy.EgressRoute {
	return proxy.EgressRoute{
		ID:                     workerconstants.LiteLLMEgressRouteID,
		PathPrefix:             workerconstants.LiteLLMPathPrefix,
		DialUpstream:           host + ":" + port,
		ClientCertPath:         workerconstants.WorkerTLSDir + "/tls.crt",
		EncryptedClientKeyPath: workerconstants.WorkerTLSDir + "/tls.key",
		SecretFilePath:         workerconstants.WorkerMTLSKeyPath,
		TrustedCACertPath:      workerconstants.WorkerTLSDir + "/ca.crt",
		ServerName:             host,
	}
}

// RegisterLiteLLMEgress registers the LiteLLM egress route on the worker's Caddy so services on
// the worker can reach LiteLLM through the control plane, which is dialed at host:port. Registering
// an existing route is a no-op, so it is safe on every (re)connect. It retries until the worker's
// Caddy answers or ctx ends (the stream closed).
func RegisterLiteLLMEgress(ctx context.Context, pm proxy.ProxyManager, workerName, host, port string) error {
	route := LiteLLMEgressRoute(host, port)

	var err error

	for attempt := 1; attempt <= liteLLMEgressAttempts; attempt++ {
		err = pm.RegisterEgressRoute(ctx, route)
		if err == nil {
			logger.InfofCtx(ctx, "WorkerGateway: registered LiteLLM egress route on worker %s", workerName)

			return nil
		}

		logger.WarningfCtx(ctx, "WorkerGateway: LiteLLM egress on worker %s (attempt %d/%d): %v",
			workerName, attempt, liteLLMEgressAttempts, err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(liteLLMEgressRetryInterval):
		}
	}

	return fmt.Errorf("gave up registering LiteLLM egress on worker %s after %d attempts: %w", workerName, liteLLMEgressAttempts, err)
}
