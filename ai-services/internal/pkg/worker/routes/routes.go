// Package routes manages worker-side proxy routes for private mTLS communication with the control plane.
package routes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	// egressAttempts and egressRetryInterval bound how long the gateway waits for a
	// joining worker's Caddy, which only starts once the worker has registered.
	egressAttempts      = 30
	egressRetryInterval = 10 * time.Second
)

// controlPlaneEgressRoute builds a worker-side route that forwards plain HTTP under pathPrefix
// over mTLS to the control-plane private ingress. The control plane is dialed at the gateway
// hostname, which the worker already resolves and which is in the gateway server certificate's SANs.
func controlPlaneEgressRoute(id, pathPrefix string) (proxy.EgressRoute, error) {
	suffix := utils.GetEnv(proxy.DomainSuffixEnvVar, "")
	if suffix == "" {
		return proxy.EgressRoute{}, fmt.Errorf("%s environment variable not set", proxy.DomainSuffixEnvVar)
	}

	host := workerconstants.WorkerGatewayName + "." + suffix
	port := utils.GetEnv(proxy.CaddyPrivateIngressPortEnvVar, proxy.DefaultPrivateIngressPort)

	return proxy.EgressRoute{
		ID:                     id,
		PathPrefix:             pathPrefix,
		DialUpstream:           host + ":" + port,
		ClientCertPath:         workerconstants.WorkerTLSDir + "/tls.crt",
		EncryptedClientKeyPath: workerconstants.WorkerTLSDir + "/tls.key",
		SecretFilePath:         workerconstants.WorkerMTLSKeyPath,
		TrustedCACertPath:      workerconstants.WorkerTLSDir + "/ca.crt",
		ServerName:             host,
	}, nil
}

// LiteLLMEgressRoute builds the worker-side route that forwards LiteLLM calls over mTLS to the
// control-plane private ingress.
func LiteLLMEgressRoute() (proxy.EgressRoute, error) {
	return controlPlaneEgressRoute(workerconstants.LiteLLMEgressRouteID, workerconstants.LiteLLMPathPrefix)
}

// CatalogAuthEgressRoute builds the worker-side route that forwards catalog service-auth calls
// (JWKS, code exchange, service-credential token) over mTLS to the control-plane private ingress.
func CatalogAuthEgressRoute() (proxy.EgressRoute, error) {
	return controlPlaneEgressRoute(workerconstants.CatalogAuthEgressRouteID, workerconstants.CatalogAuthPathPrefix)
}

// RegisterLiteLLMEgress registers the LiteLLM egress route on the worker's Caddy so services on
// the worker can reach LiteLLM through the control plane. Registering an existing route is a
// no-op, so it is safe on every (re)connect. It retries until the worker's Caddy answers or ctx
// ends (the stream closed).
func RegisterLiteLLMEgress(ctx context.Context, pm proxy.ProxyManager, workerName string) error {
	route, err := LiteLLMEgressRoute()
	if err != nil {
		return fmt.Errorf("build LiteLLM egress route for worker %s: %w", workerName, err)
	}

	return registerEgress(ctx, pm, workerName, "LiteLLM", route)
}

// RegisterCatalogAuthEgress registers the catalog-auth egress route on the worker's Caddy so
// services on the worker reach the catalog's service-auth endpoints through the control plane.
// It has the same idempotence and retry behaviour as RegisterLiteLLMEgress.
func RegisterCatalogAuthEgress(ctx context.Context, pm proxy.ProxyManager, workerName string) error {
	route, err := CatalogAuthEgressRoute()
	if err != nil {
		return fmt.Errorf("build catalog-auth egress route for worker %s: %w", workerName, err)
	}

	return registerEgress(ctx, pm, workerName, "catalog-auth", route)
}

// RegisterControlPlaneEgress registers every control-plane egress route on the worker's Caddy.
// Every route is attempted and every failure is reported.
func RegisterControlPlaneEgress(ctx context.Context, pm proxy.ProxyManager, workerName string) error {
	return errors.Join(
		RegisterLiteLLMEgress(ctx, pm, workerName),
		RegisterCatalogAuthEgress(ctx, pm, workerName),
	)
}

func registerEgress(ctx context.Context, pm proxy.ProxyManager, workerName, label string, route proxy.EgressRoute) error {
	var err error

	for attempt := 1; attempt <= egressAttempts; attempt++ {
		err = pm.RegisterEgressRoute(ctx, route)
		if err == nil {
			logger.InfofCtx(ctx, "WorkerGateway: registered %s egress route on worker %s", label, workerName)

			return nil
		}

		logger.WarningfCtx(ctx, "WorkerGateway: %s egress on worker %s (attempt %d/%d): %v",
			label, workerName, attempt, egressAttempts, err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(egressRetryInterval):
		}
	}

	return fmt.Errorf("gave up registering %s egress on worker %s after %d attempts: %w", label, workerName, egressAttempts, err)
}
