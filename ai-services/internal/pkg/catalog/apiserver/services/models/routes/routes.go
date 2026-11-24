// Package routes creates and removes the private mTLS Caddy routes that connect the control
// plane to a model running on a remote worker:
//
//	LiteLLM --http--> CP Caddy egress :8080 --mTLS--> worker Caddy ingress --http--> model pod
//
// Both routes share one path prefix, so the worker ingress can strip it and the CP egress can
// keep it. Route IDs are derived from the component ID alone, so a model can be unrouted knowing
// nothing but its ID.
package routes

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	// mtlsSecretKey is the file name of the AES key inside the mounted catalog mTLS secret.
	mtlsSecretKey = "mtls-encryption-key"

	// The control plane Caddy pod mounts the gateway PKI at workerconstants.GatewayPKIDir
	// (public/ subdirectory only, so ca.key never reaches Caddy) and the secret below.
	cpClientCertFile = "server.crt"
	cpClientKeyFile  = "server.key"
	cpCAFile         = "ca.crt"
	cpSecretDir      = "/etc/secret/" + catalogconstants.CatalogMTLSSecretName
)

// DomainResolver resolves the DOMAIN_SUFFIX and private ingress port a worker reported when it registered.
type DomainResolver interface {
	DomainSuffix(name string) (string, error)
	PrivateIngressPort(name string) string
}

// Manager registers and removes the routes of one remote model.
type Manager struct {
	workers DomainResolver

	// cpProxy returns the control plane Caddy; workerProxy returns the Caddy of the worker the
	// runtime targets. cpEgressHost returns the host LiteLLM uses to reach the control plane
	// egress. They are fields so tests can replace them.
	cpProxy      func() (proxy.ProxyManager, error)
	workerProxy  func(rt runtime.Runtime) (proxy.ProxyManager, error)
	cpEgressHost func() (string, error)
}

// NewManager creates a Manager. workers is only used by Register and may be nil for a Manager
// that only calls Unregister.
func NewManager(workers DomainResolver) *Manager {
	return &Manager{
		workers:      workers,
		cpProxy:      proxy.GetCaddyProxyManager,
		workerProxy:  remoteWorkerProxy,
		cpEgressHost: caddyHostFromAdminURL,
	}
}

// IsLocal reports whether workerName is the control-plane worker, whose models are reached
// directly on the control plane's own network and need no private routes.
func IsLocal(workerName string) bool {
	return workerName == "" || strings.EqualFold(workerName, workerconstants.LocalWorkerName)
}

// IngressRouteID is the Caddy @id of the worker-side route of a model.
func IngressRouteID(componentID uuid.UUID) string {
	return fmt.Sprintf("model-%s--mtls", componentID)
}

// EgressRouteID is the Caddy @id of the control-plane-side route of a model.
func EgressRouteID(componentID uuid.UUID) string {
	return fmt.Sprintf("model-%s--egress", componentID)
}

// PathPrefix is the URL prefix both routes of a model match.
func PathPrefix(workerName string, componentID uuid.UUID) string {
	return fmt.Sprintf("/worker/%s/models/%s", workerName, componentID)
}

// Register creates the worker ingress route to upstream (the model pod's "host:port" on the
// worker) and the control plane egress route that reaches it over mTLS. It returns the address
// the control plane uses for the model: the egress host and port followed by the route prefix,
// so "http://<returned>/v1" is the model's API base.
func (m *Manager) Register(ctx context.Context, rt runtime.Runtime, workerName string, componentID uuid.UUID, upstream string) (string, error) {
	suffix, err := m.workers.DomainSuffix(workerName)
	if err != nil {
		return "", err
	}

	workerPort := m.workers.PrivateIngressPort(workerName)
	if workerPort == "" {
		workerPort = proxy.DefaultPrivateIngressPort
	}

	// The worker certificate carries <workerName>.<domainSuffix> as its SAN, so that is both the
	// name dialed and the name verified.
	workerHost := workerName + "." + suffix

	egressHost, err := m.cpEgressHost()
	if err != nil {
		return "", err
	}

	cp, err := m.cpProxy()
	if err != nil {
		return "", fmt.Errorf("control plane caddy: %w", err)
	}

	wp, err := m.workerProxy(rt)
	if err != nil {
		return "", fmt.Errorf("worker %q caddy: %w", workerName, err)
	}

	prefix := PathPrefix(workerName, componentID)

	if err := wp.RegisterIngressRoute(ctx, proxy.IngressRoute{
		ID:         IngressRouteID(componentID),
		PathPrefix: prefix,
		Upstream:   upstream,
		Terminal:   true,
	}); err != nil {
		return "", fmt.Errorf("failed to register ingress route on worker %q: %w", workerName, err)
	}

	if err := cp.RegisterEgressRoute(ctx, proxy.EgressRoute{
		ID:                     EgressRouteID(componentID),
		PathPrefix:             prefix,
		DialUpstream:           workerHost + ":" + workerPort,
		ClientCertPath:         workerconstants.GatewayPKIDir + "/" + cpClientCertFile,
		EncryptedClientKeyPath: workerconstants.GatewayPKIDir + "/" + cpClientKeyFile,
		SecretFilePath:         cpSecretDir + "/" + mtlsSecretKey,
		TrustedCACertPath:      workerconstants.GatewayPKIDir + "/" + cpCAFile,
		ServerName:             workerHost,
	}); err != nil {
		// Do not leave a worker route that nothing on the control plane points at.
		if rbErr := wp.UnregisterRoute(context.WithoutCancel(ctx), IngressRouteID(componentID)); rbErr != nil && !isNotFound(rbErr) {
			logger.WarningfCtx(ctx, "failed to roll back ingress route %s on worker %q: %v", IngressRouteID(componentID), workerName, rbErr)
		}

		return "", fmt.Errorf("failed to register egress route on the control plane: %w", err)
	}

	return fmt.Sprintf("%s:%s%s", egressHost, proxy.PrivateEgressPort, prefix), nil
}

// Unregister removes both routes of a model. A route that does not exist is not an error, so it
// is safe to call for a model that was never routed (local worker) or whose routes are already
// gone. Both removals are attempted and every failure is reported.
func (m *Manager) Unregister(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) error {
	var errs []error

	if cp, err := m.cpProxy(); err != nil {
		errs = append(errs, fmt.Errorf("control plane caddy: %w", err))
	} else if err := cp.UnregisterRoute(ctx, EgressRouteID(componentID)); err != nil && !isNotFound(err) {
		errs = append(errs, fmt.Errorf("failed to remove egress route: %w", err))
	}

	if wp, err := m.workerProxy(rt); err != nil {
		errs = append(errs, fmt.Errorf("worker caddy: %w", err))
	} else if err := wp.UnregisterRoute(ctx, IngressRouteID(componentID)); err != nil && !isNotFound(err) {
		errs = append(errs, fmt.Errorf("failed to remove ingress route: %w", err))
	}

	return errors.Join(errs...)
}

// remoteWorkerProxy returns a ProxyManager that sends route commands to the worker rt targets.
func remoteWorkerProxy(rt runtime.Runtime) (proxy.ProxyManager, error) {
	remote, ok := rt.(*remoteruntime.RemoteRuntime)
	if !ok {
		return nil, fmt.Errorf("runtime %T is not a remote worker runtime", rt)
	}

	return proxy.NewRemoteProxyManager(remote.Sender), nil
}

// caddyHostFromAdminURL returns the hostname of the control plane Caddy pod, taken from its admin
// URL (e.g. "http://ai-services--caddy:2019"). LiteLLM reaches the egress server on that host.
func caddyHostFromAdminURL() (string, error) {
	raw := utils.GetEnv(proxy.CaddyAdminURLEnvVar, "")
	if raw == "" {
		return "", fmt.Errorf("%s environment variable not set", proxy.CaddyAdminURLEnvVar)
	}

	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("cannot derive the caddy host from %s=%q", proxy.CaddyAdminURLEnvVar, raw)
	}

	return u.Hostname(), nil
}

// isNotFound reports a missing route. A remote worker returns the error as text, so it cannot be
// matched with errors.Is alone.
func isNotFound(err error) bool {
	return errors.Is(err, proxy.ErrRouteNotFound) || strings.Contains(err.Error(), proxy.ErrRouteNotFound.Error())
}
