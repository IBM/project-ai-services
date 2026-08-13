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
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

const (
	// commandChannelSize is the buffer size for the per-worker command channel.
	commandChannelSize = 32

	// tokenTTLHours is the validity window for single-use bootstrap tokens.
	tokenTTLHours = 24
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
	mu      sync.RWMutex
	workers map[string]*WorkerEntry
	repo    repository.WorkerRepository // may be nil in tests
}

// New creates a new Registry backed by the given WorkerRepository.
// Pass nil for tests that do not need DB persistence.
func New(repo repository.WorkerRepository) *Registry {
	return &Registry{
		workers: make(map[string]*WorkerEntry),
		repo:    repo,
	}
}

// Register upserts the worker into the DB (persisting name, metadata, status=ready)
// and ensures an in-memory entry with a live CommandCh exists.
// Metadata from the RegisterRequest is stored in the DB
// metadata JSON column directly — no separate in-memory field is needed.
func (r *Registry) Register(ctx context.Context, req *workerpb.RegisterRequest) (*WorkerEntry, error) {
	workerName := req.GetWorkerName()

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
		status := models.WorkerStatusReady
		w := &models.Worker{
			Name:        workerName,
			RuntimeType: models.WorkerRuntimeTypePodman,
			Status:      status,
			Metadata:    metadataToAny(req.GetMetadata()),
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
	_, ok := r.workers[workerName]
	if ok {
		delete(r.workers, workerName)
	}
	r.mu.Unlock()

	if ok && r.repo != nil {
		status := models.WorkerStatusDisconnected
		if err := r.repo.Update(ctx, workerName, repository.WorkerUpdate{Status: &status}); err != nil {
			logger.WarningfCtx(ctx, "worker registry: DB disconnect update failed for %s: %v", workerName, err)
		}
	}
}

// Deregister removes the worker from the in-memory map and hard-deletes its row from the DB.
// Use this when a worker is permanently decommissioned, not just temporarily offline.
func (r *Registry) Deregister(ctx context.Context, workerName string) error {
	r.mu.Lock()
	entry, ok := r.workers[workerName]
	if ok {
		delete(r.workers, workerName)
	}
	r.mu.Unlock()

	if r.repo != nil && ok && entry.DBID != uuid.Nil {
		if err := r.repo.Delete(ctx, entry.DBID); err != nil {
			return fmt.Errorf("worker registry: DB delete failed for %s: %w", workerName, err)
		}
	}

	return nil
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

// ──────────────────────────────────────────────────────────────────────────────
// Bootstrap token store
// ──────────────────────────────────────────────────────────────────────────────

// TokenRecord holds a single-use bootstrap token.
type TokenRecord struct {
	Token     string
	ExpiresAt time.Time
	Used      bool
}

// TokenStore is an in-memory single-use bootstrap token store.
type TokenStore struct {
	mu     sync.Mutex
	tokens map[string]*TokenRecord
}

// NewTokenStore creates an empty token store.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*TokenRecord)}
}

// IssueToken generates a new 24-hour single-use token and returns it.
func (ts *TokenStore) IssueToken() string {
	token := uuid.NewString()
	ts.mu.Lock()
	ts.tokens[token] = &TokenRecord{
		Token:     token,
		ExpiresAt: time.Now().Add(tokenTTLHours * time.Hour),
	}
	ts.mu.Unlock()

	return token
}

// Validate checks token validity and marks it used. Returns an error if the
// token is unknown, already used, or expired.
func (ts *TokenStore) Validate(token string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	rec, ok := ts.tokens[token]
	if !ok {
		return fmt.Errorf("bootstrap token not found")
	}
	if rec.Used {
		return fmt.Errorf("bootstrap token already used")
	}
	if time.Now().After(rec.ExpiresAt) {
		return fmt.Errorf("bootstrap token expired")
	}
	rec.Used = true

	return nil
}
