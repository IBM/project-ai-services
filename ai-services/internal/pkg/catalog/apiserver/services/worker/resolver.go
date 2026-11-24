// Package worker resolves the worker a deployment targets and the runtime used to reach it.
// It is shared by the application and model deployment flows so both treat workers the same way:
// every deployment, including one on the local worker, goes through a connected worker in the registry.
package worker

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// Resolver resolves the worker a deployment targets and the runtime used to reach it.
type Resolver interface {
	// ID returns the database ID the named worker was assigned when it registered.
	ID(name string) (uuid.UUID, bool)
	// Info returns the name and runtime type of the worker with the given database ID. It also
	// finds workers that are no longer connected. The name is empty when the worker is unknown.
	Info(ctx context.Context, id uuid.UUID) (name, runtimeType string, err error)
	// Validate confirms the named worker is connected.
	Validate(ctx context.Context, name string) error
	// RuntimeType returns the named worker's registered runtime type.
	RuntimeType(ctx context.Context, name string) (string, error)
	// DomainSuffix returns the DOMAIN_SUFFIX the named worker reported when it registered. It is
	// what, with the worker name, forms the hostname the control plane dials for private traffic.
	DomainSuffix(name string) (string, error)
	// PrivateIngressPort returns the private ingress port the named worker reported when it registered,
	// or proxy.DefaultPrivateIngressPort if unset.
	PrivateIngressPort(name string) string
	// NewRuntime returns a runtime that forwards every call to the named worker. namespace
	// scopes namespace-aware operations on OpenShift workers; Podman workers ignore it.
	NewRuntime(ctx context.Context, name, namespace string) (runtime.Runtime, error)
}

// registryResolver is the Resolver backed by the worker registry.
type registryResolver struct {
	registry stream.WorkerRegistry
}

// NewResolver creates a Resolver backed by reg. reg may be nil, in which case every
// lookup fails as if no worker were connected.
func NewResolver(reg stream.WorkerRegistry) Resolver {
	return &registryResolver{registry: reg}
}

// DefaultName returns name, or the local worker's name when name is empty.
func DefaultName(name string) string {
	if name == "" {
		return workerconstants.LocalWorkerName
	}

	return name
}

// ID returns the database ID the named worker was assigned when it registered.
func (r *registryResolver) ID(name string) (uuid.UUID, bool) {
	if r.registry == nil {
		return uuid.Nil, false
	}

	return r.registry.WorkerID(name)
}

// Info returns the name and runtime type of the worker with the given database ID. The registry
// falls back to the database for workers that are not connected.
func (r *registryResolver) Info(ctx context.Context, id uuid.UUID) (string, string, error) {
	if r.registry == nil {
		return "", "", fmt.Errorf("worker registry is not configured on this server")
	}

	name, rt := r.registry.WorkerInfoByID(ctx, id)

	return name, rt, nil
}

// Validate confirms the named worker is connected.
func (r *registryResolver) Validate(ctx context.Context, name string) error {
	if r.registry == nil {
		return fmt.Errorf("worker registry is not configured on this server")
	}

	if !r.registry.IsWorkerConnected(ctx, name) {
		return fmt.Errorf("worker %q is not connected", name)
	}

	return nil
}

// RuntimeType returns the named worker's registered runtime type, after confirming it is connected.
func (r *registryResolver) RuntimeType(ctx context.Context, name string) (string, error) {
	if err := r.Validate(ctx, name); err != nil {
		return "", err
	}

	rt, ok := r.registry.WorkerRuntimeType(name)
	if !ok || rt == "" {
		return "", fmt.Errorf("worker %q runtime type not available", name)
	}

	if !runtimeTypes.RuntimeType(rt).Valid() {
		return "", fmt.Errorf("worker %q has unsupported runtime type %q", name, rt)
	}

	return rt, nil
}

// DomainSuffix returns the DOMAIN_SUFFIX the named worker reported at registration.
func (r *registryResolver) DomainSuffix(name string) (string, error) {
	if r.registry == nil {
		return "", fmt.Errorf("worker registry is not configured on this server")
	}

	meta, ok := r.registry.WorkerMetadata(name)
	if !ok {
		return "", fmt.Errorf("worker %q is not connected", name)
	}

	suffix := meta[workerconstants.MetaKeyDomainSuffix]
	if suffix == "" {
		return "", fmt.Errorf("worker %q did not report a domain suffix; re-join it with DOMAIN_SUFFIX set", name)
	}

	return suffix, nil
}

// PrivateIngressPort returns the private ingress port the named worker reported, or proxy.DefaultPrivateIngressPort if unset.
func (r *registryResolver) PrivateIngressPort(name string) string {
	if r.registry == nil {
		return proxy.DefaultPrivateIngressPort
	}

	meta, ok := r.registry.WorkerMetadata(name)
	if !ok {
		return proxy.DefaultPrivateIngressPort
	}

	port := meta[workerconstants.MetaKeyPrivateIngressPort]
	if port == "" {
		return proxy.DefaultPrivateIngressPort
	}

	return port
}

// NewRuntime returns a runtime that forwards every call to the named worker over the gRPC
// CommandStream. namespace scopes namespace-aware operations on OpenShift workers; Podman
// workers ignore it.
func (r *registryResolver) NewRuntime(ctx context.Context, name, namespace string) (runtime.Runtime, error) {
	rt, err := r.RuntimeType(ctx, name)
	if err != nil {
		return nil, err
	}

	remote, err := runtime.NewRuntimeFactory(runtimeTypes.RuntimeType(rt)).CreateRemote(name, r.registry, namespace)
	if err != nil {
		return nil, fmt.Errorf("create remote runtime for worker %q: %w", name, err)
	}

	return remote, nil
}
