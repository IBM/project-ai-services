package applicationservice

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// DeploymentRegistry tracks in-flight deployments so they can be cancelled
// when a delete request arrives mid-deployment.
type DeploymentRegistry struct {
	mu      sync.Mutex
	entries map[uuid.UUID]context.CancelFunc
}

// NewDeploymentRegistry creates a new registry.
func NewDeploymentRegistry() *DeploymentRegistry {
	return &DeploymentRegistry{
		entries: make(map[uuid.UUID]context.CancelFunc),
	}
}

// Register stores a cancel function for the given application and returns
// a cancellable context derived from the provided parent.
func (r *DeploymentRegistry) Register(parent context.Context, appID uuid.UUID) context.Context {
	ctx, cancel := context.WithCancel(parent)

	r.mu.Lock()
	r.entries[appID] = cancel
	r.mu.Unlock()

	return ctx
}

// Cancel signals the in-flight deployment for appID to stop and removes it
// from the registry. It is a no-op if appID is not registered.
func (r *DeploymentRegistry) Cancel(appID uuid.UUID) {
	r.mu.Lock()
	cancel, ok := r.entries[appID]
	if ok {
		delete(r.entries, appID)
	}
	r.mu.Unlock()

	if ok {
		cancel()
	}
}

// Deregister removes a completed deployment from the registry and calls
// cancel() to release the child context node from the parent's tree.
// Not calling cancel() would leak the context until the parent (context.Background)
// is itself cancelled — which never happens.
func (r *DeploymentRegistry) Deregister(appID uuid.UUID) {
	r.mu.Lock()
	cancel, ok := r.entries[appID]
	delete(r.entries, appID)
	r.mu.Unlock()

	if ok {
		cancel()
	}
}
