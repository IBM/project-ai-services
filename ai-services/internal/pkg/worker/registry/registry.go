// Package registry manages the set of currently-connected workers.
// It holds only the in-process gRPC plumbing (command channel + result routing)
// that cannot live in the database. All durable worker state (status, metadata,
// heartbeat) is owned by the WorkerRepository.
package registry

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// ErrWorkerAlreadyActive is returned by Register when the named worker already
// has an active in-memory entry (i.e. a live CommandStream is open).
var ErrWorkerAlreadyActive = fmt.Errorf("worker already active")

// ErrWorkerNotFound is returned by Restore when the named worker has no DB row,
// or its status is pending (never completed bootstrap).
var ErrWorkerNotFound = fmt.Errorf("worker not found")

// ErrUnsupportedRuntimeType is returned by Register when runtimeType is not a
// recognised value (podman or openshift).
var ErrUnsupportedRuntimeType = fmt.Errorf("unsupported runtime_type")

// validRuntimeTypes is the list of runtime type strings accepted by Register.
var validRuntimeTypes = []models.WorkerRuntimeType{
	models.WorkerRuntimeTypePodman,
	models.WorkerRuntimeTypeOpenShift,
}

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

	WorkerName  string
	RuntimeType string
	Metadata    map[string]string

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
func (r *Registry) Register(ctx context.Context, workerName, runtimeType string, metadata map[string]string) (*WorkerEntry, error) {
	if !slices.Contains(validRuntimeTypes, models.WorkerRuntimeType(runtimeType)) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedRuntimeType, runtimeType)
	}

	r.mu.Lock()
	if _, exists := r.workers[workerName]; exists {
		r.mu.Unlock()

		return nil, fmt.Errorf("%w: %s", ErrWorkerAlreadyActive, workerName)
	}

	entry := &WorkerEntry{
		WorkerName:  workerName,
		RuntimeType: runtimeType,
		Metadata:    metadata,
		CommandCh:   make(chan *workerpb.Command, commandChannelSize),
		results:     make(map[string]chan *workerpb.CommandResult),
	}
	r.workers[workerName] = entry
	r.mu.Unlock()

	if r.repo != nil {
		w := &models.Worker{
			Name:        workerName,
			RuntimeType: models.WorkerRuntimeType(runtimeType),
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

// Restore re-creates the in-memory entry for a previously-registered worker whose
// record exists in the DB but whose in-memory entry is absent (e.g. after a
// control-plane restart, or after a worker reconnects following a clean disconnect).
// The caller must have already verified the worker's identity via mTLS — this
// method does not consult the token store.
//
// Returns ErrWorkerNotFound if the worker has no DB row or its status is pending
// (it was pre-registered but never completed the bootstrap flow; a cert alone is
// not sufficient proof of identity in that state).
//
// If the worker is already in the in-memory map (concurrent reconnect), the
// existing entry is returned unchanged — Restore is idempotent.
//
// On success the DB status is updated to ready and the new entry is returned.
func (r *Registry) Restore(ctx context.Context, workerName string) (*WorkerEntry, error) {
	// Fast path: already in memory (e.g. concurrent reconnect races).
	r.mu.RLock()
	if entry, ok := r.workers[workerName]; ok {
		r.mu.RUnlock()

		return entry, nil
	}
	r.mu.RUnlock()

	if r.repo == nil {
		return nil, ErrWorkerNotFound
	}

	w, err := r.repo.GetByName(ctx, workerName)
	if err != nil {
		return nil, fmt.Errorf("worker registry: DB lookup for %s: %w", workerName, err)
	}
	// Pending means the worker was pre-registered but never completed the initial
	// bootstrap (Register RPC + token exchange). Require the full flow.
	if w == nil || w.Status == models.WorkerStatusPending {
		return nil, ErrWorkerNotFound
	}

	entry := &WorkerEntry{
		DBID:        w.ID,
		WorkerName:  workerName,
		RuntimeType: string(w.RuntimeType),
		Metadata:    metadataToString(w.Metadata),
		CommandCh:   make(chan *workerpb.Command, commandChannelSize),
		results:     make(map[string]chan *workerpb.CommandResult),
	}

	r.mu.Lock()
	// Re-check under write lock to guard against a concurrent Restore.
	if existing, ok := r.workers[workerName]; ok {
		r.mu.Unlock()

		return existing, nil
	}
	r.workers[workerName] = entry
	r.mu.Unlock()

	if err := r.repo.Update(ctx, w.ID, repository.WorkerUpdate{Status: utils.Ptr(models.WorkerStatusReady), Message: utils.Ptr("")}); err != nil {
		logger.WarningfCtx(ctx, "worker registry: DB status update failed on restore for %s: %v", workerName, err)
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
		msg := MsgDisconnected
		if err := r.repo.Update(ctx, entry.DBID, repository.WorkerUpdate{
			Status:  utils.Ptr(models.WorkerStatusDisconnected),
			Message: &msg,
		}); err != nil {
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
		// Pending workers have never connected — they have no heartbeat yet
		// and must not be swept to Disconnected.
		if w.Status == models.WorkerStatusPending || w.Status == models.WorkerStatusDisconnected {
			continue
		}
		if w.LastHeartbeat == nil || now.Sub(*w.LastHeartbeat) > timeout {
			logger.WarningfCtx(ctx, "worker registry: worker %s heartbeat timed out — marking disconnected", w.Name)
			msg := MsgLostHeartbeat
			if err := r.repo.Update(ctx, w.ID, repository.WorkerUpdate{
				Status:  utils.Ptr(models.WorkerStatusDisconnected),
				Message: &msg,
			}); err != nil {
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

// WorkerCommandChannel returns the command channel for the named worker.
// It satisfies the stream.WorkerRegistry interface.
func (r *Registry) WorkerCommandChannel(workerName string) (chan *workerpb.Command, bool) {
	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}

	return entry.CommandCh, true
}

// WorkerRuntimeType returns the runtime type string for the named worker.
// It satisfies the stream.WorkerRegistry interface.
func (r *Registry) WorkerRuntimeType(workerName string) (string, bool) {
	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}

	return entry.RuntimeType, true
}

// WorkerMetadata returns the metadata map for the named worker.
// It satisfies the stream.WorkerRegistry interface.
func (r *Registry) WorkerMetadata(workerName string) (map[string]string, bool) {
	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}

	return entry.Metadata, true
}

// WorkerID returns the database UUID assigned to the named worker at registration
// time. Returns (uuid.Nil, false) if the worker is not currently connected.
func (r *Registry) WorkerID(workerName string) (uuid.UUID, bool) {
	r.mu.RLock()
	entry, ok := r.workers[workerName]
	r.mu.RUnlock()
	if !ok {
		return uuid.Nil, false
	}

	return entry.DBID, true
}

// WorkerNameByID returns the name of the worker whose DBID matches id.
// Scans the in-memory map under a read lock — O(n) but n is tiny in practice.
// Returns ("", false) if no connected worker has that ID.
func (r *Registry) WorkerNameByID(id uuid.UUID) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, entry := range r.workers {
		if entry.DBID == id {
			return name, true
		}
	}

	return "", false
}

// IsWorkerConnected checks the in-memory cache first, then confirms status=ready
// in the DB. Returns false if the worker is absent from the cache, not found in
// the DB, or has any status other than ready.
// When repo is nil (New was called without a DB — tests only) the cache check
// is the sole authority.
// It satisfies the stream.WorkerRegistry interface.
func (r *Registry) IsWorkerConnected(ctx context.Context, workerName string) bool {
	r.mu.RLock()
	_, inCache := r.workers[workerName]
	r.mu.RUnlock()

	if !inCache {
		return false
	}

	if r.repo == nil {
		return true
	}

	w, err := r.repo.GetByName(ctx, workerName)
	if err != nil || w == nil {
		return false
	}

	return w.Status == models.WorkerStatusReady
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

// metadataToString converts a map[string]any (from the DB model) back to map[string]string
// for use in a WorkerEntry. Non-string values are converted via fmt.Sprintf.
func metadataToString(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprintf("%v", v)
		}
	}

	return out
}

// ValidateToken checks a bootstrap token, marks it used, and returns the worker name it was
// issued for. Exposed on Registry so callers (gateway, tests) do not need to hold a
// separate TokenStore reference.
func (r *Registry) ValidateToken(token string) (string, error) {
	return r.tokenStore.Validate(token)
}
