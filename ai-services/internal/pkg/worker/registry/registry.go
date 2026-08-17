// Package registry manages the set of currently-connected workers.
// It holds only the in-process gRPC plumbing (command channel + result routing)
// that cannot live in the database. All durable worker state (status, metadata,
// heartbeat) is owned by the WorkerRepository.
package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

const (
	// commandChannelSize is the buffer size for the per-worker command channel.
	// Each concurrent deployment to the same worker writes one command to this
	// channel. A buffer large enough to hold all in-flight deployments prevents
	// HTTP handlers from blocking while the gRPC stream drains the queue.
	// 32 allows up to 32 simultaneous deployments targeting the same worker
	// without any back-pressure on the HTTP layer.
	commandChannelSize = 32
)

// WorkerEntry holds the in-process gRPC plumbing for a single connected worker.
// Durable fields (status, metadata, heartbeat) live in the DB, not here.
type WorkerEntry struct {
	// DBID is the UUID assigned by the database after the first Upsert.
	// It is zero-value until the first successful DB upsert.
	DBID uuid.UUID

	WorkerName string

	// CommandCh is written by RemoteRuntime to send commands to this worker.
	// The gateway goroutine reads from it and writes to the gRPC stream.
	CommandCh chan *workerpb.Command

	resultsMu sync.Mutex
	results   map[string]chan *workerpb.CommandResult
}

// waitForResult registers a result channel for commandID and returns it.
func (w *WorkerEntry) waitForResult(commandID string) chan *workerpb.CommandResult {
	ch := make(chan *workerpb.CommandResult, 1)
	w.resultsMu.Lock()
	w.results[commandID] = ch
	w.resultsMu.Unlock()

	return ch
}

// deliverResult routes an incoming result to the waiting caller.
func (w *WorkerEntry) deliverResult(res *workerpb.CommandResult) {
	id := res.GetCommandId()
	w.resultsMu.Lock()
	ch, ok := w.results[id]
	if ok {
		delete(w.results, id)
	}
	w.resultsMu.Unlock()
	if ok {
		select {
		case ch <- res:
		default:
		}
	}
}

// Registry tracks all currently-connected workers by name.
type Registry struct {
	mu         sync.RWMutex
	workers    map[string]*WorkerEntry
	repo       repository.WorkerRepository // may be nil in tests
	tokenStore *TokenStore
}

// New creates a new Registry backed by the given WorkerRepository.
// Pass nil for tests that do not need DB persistence.
func New(repo repository.WorkerRepository) *Registry {
	return &Registry{
		workers:    make(map[string]*WorkerEntry),
		repo:       repo,
		tokenStore: NewTokenStore(),
	}
}

// Register upserts the worker into the DB (status=ready, with provided metadata)
// and ensures an in-memory entry with a live CommandCh exists.
// workerName must come from the validated token — callers must not trust the name
// the worker declares in its RegisterRequest.
// runtimeType must be one of the supported values ("podman", "openshift");
// an unsupported or empty value is rejected with an error.
func (r *Registry) Register(ctx context.Context, workerName, runtimeType string, metadata map[string]string) (*WorkerEntry, error) {
	rt, err := runtimeTypeFromString(runtimeType)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	entry, exists := r.workers[workerName]
	if !exists {
		entry = &WorkerEntry{
			WorkerName: workerName,
			CommandCh:  make(chan *workerpb.Command, commandChannelSize),
			results:    make(map[string]chan *workerpb.CommandResult),
		}
		r.workers[workerName] = entry
	}
	r.mu.Unlock()

	if r.repo != nil {
		w := &models.Worker{
			Name:        workerName,
			RuntimeType: rt,
			Status:      models.WorkerStatusReady,
			Metadata:    metadataToAny(metadata),
		}
		if err := r.repo.Upsert(ctx, w); err != nil {
			logger.WarningfCtx(ctx, "worker registry: DB upsert failed for %s: %v", workerName, err)
		} else {
			r.mu.Lock()
			entry.DBID = w.ID
			r.mu.Unlock()
		}
	}

	return entry, nil
}

// Preregister creates a pending DB row for a named worker and returns a single-use
// bootstrap token the operator passes to the worker daemon at startup.
// If a row already exists (re-registration), it is reset to pending and a new token
// supersedes the old one. The registry's in-memory map is not touched — the worker
// is not "connected" until it calls Register via gRPC.
func (r *Registry) Preregister(ctx context.Context, workerName string) (string, error) {
	if r.repo == nil {
		return "", fmt.Errorf("worker registry: no repository configured")
	}

	w := &models.Worker{
		Name:        workerName,
		RuntimeType: models.WorkerRuntimeTypeUnknown,
		Status:      models.WorkerStatusPending,
	}
	if err := r.repo.Upsert(ctx, w); err != nil {
		return "", fmt.Errorf("worker registry: DB upsert failed for %s: %w", workerName, err)
	}

	return r.tokenStore.IssueToken(workerName), nil
}

// List returns all worker rows from the database ordered by registered_at ascending.
func (r *Registry) List(ctx context.Context) ([]models.Worker, error) {
	if r.repo == nil {
		return nil, nil
	}

	return r.repo.GetAll(ctx)
}

// Get returns the in-memory entry for a connected worker, or false if not found.
func (r *Registry) Get(workerName string) (*WorkerEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.workers[workerName]

	return e, ok
}

// Disconnect removes the worker from the in-memory map and marks it disconnected in the DB.
// The DB row is kept so the worker can reconnect and its history is preserved.
func (r *Registry) Disconnect(ctx context.Context, workerName string) {
	r.mu.Lock()
	entry, ok := r.workers[workerName]
	if ok {
		delete(r.workers, workerName)
	}
	r.mu.Unlock()

	if ok && r.repo != nil && entry.DBID != uuid.Nil {
		if err := r.repo.Update(ctx, entry.DBID, repository.WorkerUpdate{Status: utils.Ptr(models.WorkerStatusDisconnected)}); err != nil {
			logger.WarningfCtx(ctx, "worker registry: DB disconnect update failed for %s: %v", workerName, err)
		}
	}
}

// SweepStale fetches all workers from the DB and marks any whose last heartbeat
// has exceeded timeout as disconnected. It is called by the gateway sweeper.
func (r *Registry) SweepStale(ctx context.Context, timeout time.Duration) {
	if r.repo == nil {
		return
	}

	workers, err := r.repo.GetAll(ctx)
	if err != nil {
		logger.WarningfCtx(ctx, "worker registry: sweeper failed to fetch workers: %v", err)

		return
	}

	now := time.Now()

	for _, w := range workers {
		if w.Status == models.WorkerStatusDisconnected {
			continue
		}
		if w.LastHeartbeat == nil || now.Sub(*w.LastHeartbeat) > timeout {
			logger.WarningfCtx(ctx, "worker registry: worker %s heartbeat timed out — marking disconnected", w.Name)
			if err := r.repo.Update(ctx, w.ID, repository.WorkerUpdate{Status: utils.Ptr(models.WorkerStatusDisconnected)}); err != nil {
				logger.WarningfCtx(ctx, "worker registry: failed to update stale worker %s: %v", w.Name, err)
			}
		}
	}
}

// UpdateHeartbeat writes the current timestamp to last_heartbeat in the DB for the
// named worker. It is called by the gateway on every heartbeat message.
func (r *Registry) UpdateHeartbeat(ctx context.Context, workerName string) {
	if r.repo == nil {
		return
	}

	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()

	if !ok || entry.DBID == uuid.Nil {
		return
	}

	now := time.Now()
	if err := r.repo.Update(ctx, entry.DBID, repository.WorkerUpdate{LastHeartbeat: &now}); err != nil {
		logger.WarningfCtx(ctx, "worker registry: heartbeat update failed for %s: %v", workerName, err)
	}
}

// Deregister removes the worker from the in-memory map and hard-deletes its DB row by UUID.
// Use this when a worker is permanently decommissioned, not just temporarily offline.
// Returns (true, nil) if a row was deleted, (false, nil) if not found.
func (r *Registry) Deregister(ctx context.Context, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	for name, entry := range r.workers {
		if entry.DBID == id {
			delete(r.workers, name)

			break
		}
	}
	r.mu.Unlock()

	if r.repo == nil {
		return false, nil
	}

	deleted, err := r.repo.Delete(ctx, id)
	if err != nil {
		return false, fmt.Errorf("worker registry: DB delete failed for %s: %w", id, err)
	}

	return deleted, nil
}

// DeliverResult routes an incoming CommandResult to the waiting RemoteRuntime call.
func (r *Registry) DeliverResult(res *workerpb.CommandResult) {
	r.mu.RLock()
	entry, ok := r.workers[res.GetWorkerName()]
	r.mu.RUnlock()
	if ok {
		entry.deliverResult(res)
	}
}

// WaitForResult returns a channel that will receive the result for commandID on workerName.
func (r *Registry) WaitForResult(workerName, commandID string) (chan *workerpb.CommandResult, error) {
	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("worker %s not connected", workerName)
	}

	return entry.waitForResult(commandID), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// metadataToAny converts a map[string]string (from proto) to map[string]any (for the DB model).
func metadataToAny(m map[string]string) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

// runtimeTypeFromString maps a runtime type string declared by the worker to the
// corresponding DB model constant. Returns an error for unsupported or empty values.
// Supported values: "podman", "openshift".
func runtimeTypeFromString(s string) (models.WorkerRuntimeType, error) {
	switch s {
	case string(models.WorkerRuntimeTypePodman):
		return models.WorkerRuntimeTypePodman, nil
	case string(models.WorkerRuntimeTypeOpenShift):
		return models.WorkerRuntimeTypeOpenShift, nil
	default:
		return "", fmt.Errorf("unsupported runtime_type %q: must be %q or %q",
			s, models.WorkerRuntimeTypePodman, models.WorkerRuntimeTypeOpenShift)
	}
}

// ValidateToken checks a bootstrap token, marks it used, and returns the worker name it was
// issued for. Exposed on Registry so callers (gateway, tests) do not need to hold a
// separate TokenStore reference.
func (r *Registry) ValidateToken(token string) (string, error) {
	return r.tokenStore.Validate(token)
}
