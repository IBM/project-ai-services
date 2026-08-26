package registry

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// ──────────────────────────────────────────────────────────────────────────────
// Minimal fake repo (no DB) used by tests that need persistence behaviour.
// ──────────────────────────────────────────────────────────────────────────────

type fakeWorkerRepo struct {
	workers map[string]*models.Worker
	byID    map[uuid.UUID]*models.Worker
}

func newFakeWorkerRepo() *fakeWorkerRepo {
	return &fakeWorkerRepo{
		workers: make(map[string]*models.Worker),
		byID:    make(map[uuid.UUID]*models.Worker),
	}
}

func (r *fakeWorkerRepo) Upsert(_ context.Context, w *models.Worker) error {
	if existing, ok := r.workers[w.Name]; ok {
		w.ID = existing.ID
	} else {
		w.ID = uuid.New()
	}
	w.RegisteredAt = time.Now()
	w.UpdatedAt = time.Now()
	cp := *w
	r.workers[w.Name] = &cp
	r.byID[w.ID] = &cp
	return nil
}

func (r *fakeWorkerRepo) Update(_ context.Context, id uuid.UUID, u repository.WorkerUpdate) error {
	w, ok := r.byID[id]
	if !ok {
		return nil
	}
	if u.Status != nil {
		w.Status = *u.Status
	}
	if u.LastHeartbeat != nil {
		w.LastHeartbeat = u.LastHeartbeat
	}
	return nil
}

func (r *fakeWorkerRepo) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	w, ok := r.byID[id]
	if !ok {
		return false, nil
	}
	delete(r.workers, w.Name)
	delete(r.byID, id)
	return true, nil
}

func (r *fakeWorkerRepo) GetAll(_ context.Context) ([]models.Worker, error) {
	out := make([]models.Worker, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, *w)
	}
	return out, nil
}

var _ repository.WorkerRepository = (*fakeWorkerRepo)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// Preregister tests
// ──────────────────────────────────────────────────────────────────────────────

func TestRegistry_Preregister_IssuesToken(t *testing.T) {
	reg := New(newFakeWorkerRepo())

	token, err := reg.Preregister(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("Preregister: unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestRegistry_Preregister_CreatesPendingRow(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	if _, err := reg.Preregister(context.Background(), "worker-a"); err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	w, ok := repo.workers["worker-a"]
	if !ok {
		t.Fatal("expected a DB row for worker-a")
	}
	if w.Status != models.WorkerStatusPending {
		t.Errorf("expected status %q, got %q", models.WorkerStatusPending, w.Status)
	}
}

func TestRegistry_Preregister_NoRepo(t *testing.T) {
	reg := New(nil)

	if _, err := reg.Preregister(context.Background(), "worker-a"); err == nil {
		t.Fatal("expected error when no repository is configured")
	}
}

func TestRegistry_Preregister_TokenIsValidatable(t *testing.T) {
	reg := New(newFakeWorkerRepo())

	token, err := reg.Preregister(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	name, err := reg.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if name != "worker-a" {
		t.Errorf("expected %q, got %q", "worker-a", name)
	}
}

func TestRegistry_Preregister_TokenNotReusable(t *testing.T) {
	reg := New(newFakeWorkerRepo())

	token, err := reg.Preregister(context.Background(), "worker-a")
	if err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	if _, err := reg.ValidateToken(token); err != nil {
		t.Fatalf("first ValidateToken: %v", err)
	}

	if _, err := reg.ValidateToken(token); err == nil {
		t.Fatal("second ValidateToken: expected error for already-used token")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Registry tests (nil repo — no DB)
// ──────────────────────────────────────────────────────────────────────────────

func TestRegistry_RegisterAddsEntry(t *testing.T) {
	reg := New(nil)

	entry, err := reg.Register(context.Background(), "worker-1", "podman", nil)
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.CommandCh == nil {
		t.Fatal("expected non-nil CommandCh")
	}
	if entry.WorkerName != "worker-1" {
		t.Errorf("expected WorkerName %q, got %q", "worker-1", entry.WorkerName)
	}
}

func TestRegistry_Register_InvalidRuntimeType(t *testing.T) {
	reg := New(nil)

	// runtimeType is stored as-is; callers are responsible for validation.
	// Registering with an arbitrary type must succeed without error.
	entry, err := reg.Register(context.Background(), "worker-1", "docker", nil)
	if err != nil {
		t.Fatalf("Register with unknown runtime_type: unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
}

func TestRegistry_RegisterIdempotent(t *testing.T) {
	reg := New(nil)

	e1, err := reg.Register(context.Background(), "worker-1", "podman", nil)
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// A second Register for the same name returns ErrWorkerAlreadyActive.
	_, err = reg.Register(context.Background(), "worker-1", "podman", nil)
	if err == nil {
		t.Fatal("expected ErrWorkerAlreadyActive on second Register, got nil")
	}

	// The original entry must still be retrievable.
	e2, ok := reg.Get("worker-1")
	if !ok {
		t.Fatal("expected worker-1 to still be in the registry")
	}
	if e1 != e2 {
		t.Error("expected same in-memory entry after duplicate Register attempt")
	}
}

func TestRegistry_GetKnownWorker(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	entry, ok := reg.Get("worker-1")
	if !ok {
		t.Fatal("expected to find worker-1")
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
}

func TestRegistry_GetUnknownWorker(t *testing.T) {
	reg := New(nil)

	_, ok := reg.Get("ghost")
	if ok {
		t.Fatal("expected not to find unknown worker")
	}
}

func TestRegistry_Disconnect(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	reg.Disconnect(context.Background(), "worker-1")

	_, ok := reg.Get("worker-1")
	if ok {
		t.Error("expected worker-1 to be removed after Disconnect")
	}
}

func TestRegistry_DisconnectUnknownIsNoop(t *testing.T) {
	reg := New(nil)
	// Must not panic.
	reg.Disconnect(context.Background(), "never-registered")
}

func TestRegistry_DeregisterUnknown(t *testing.T) {
	reg := New(nil)

	deleted, err := reg.Deregister(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("Deregister: unexpected error: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false for unknown UUID")
	}
}

func TestRegistry_WaitForResult_WorkerNotConnected(t *testing.T) {
	reg := New(nil)

	_, err := reg.WaitForResult("ghost", "cmd-1")
	if err == nil {
		t.Fatal("expected error for unconnected worker")
	}
}

func TestRegistry_DeliverResult_Routing(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	ch, err := reg.WaitForResult("worker-1", "cmd-42")
	if err != nil {
		t.Fatalf("WaitForResult: %v", err)
	}

	res := &workerpb.CommandResult{
		WorkerName: "worker-1",
		CommandId:  "cmd-42",
		Success:    true,
	}
	reg.DeliverResult(res)

	select {
	case got := <-ch:
		if got.GetCommandId() != "cmd-42" {
			t.Errorf("expected command_id %q, got %q", "cmd-42", got.GetCommandId())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
}

func TestRegistry_DeliverResult_NoWaiter(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	// Delivering a result with no waiter must not block or panic.
	reg.DeliverResult(&workerpb.CommandResult{
		WorkerName: "worker-1",
		CommandId:  "orphan",
	})
}

func TestRegistry_DeliverResult_UnknownWorker(t *testing.T) {
	reg := New(nil)
	// Delivering for a worker that has never registered must not panic.
	reg.DeliverResult(&workerpb.CommandResult{
		WorkerName: "ghost",
		CommandId:  "cmd-1",
	})
}

func TestRegistry_ValidateToken(t *testing.T) {
	reg := New(nil)
	token := reg.tokenStore.IssueToken("worker-x")

	name, err := reg.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if name != "worker-x" {
		t.Errorf("expected %q, got %q", "worker-x", name)
	}
}

func TestRegistry_ValidateToken_Invalid(t *testing.T) {
	reg := New(nil)

	if _, err := reg.ValidateToken("garbage"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SweepStale tests
// ──────────────────────────────────────────────────────────────────────────────

func TestRegistry_SweepStale_PendingNotSwept(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	// Pre-register creates a pending row with no heartbeat.
	if _, err := reg.Preregister(context.Background(), "worker-pending"); err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	// Sweep with a zero timeout — would sweep anything with a nil heartbeat.
	reg.SweepStale(context.Background(), 0)

	workers, _ := repo.GetAll(context.Background())
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].Status != models.WorkerStatusPending {
		t.Errorf("expected status %q, got %q", models.WorkerStatusPending, workers[0].Status)
	}
}

func TestRegistry_SweepStale_StaleReadyWorkerSwept(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	// Insert a ready worker whose heartbeat is already in the past.
	past := time.Now().Add(-2 * time.Minute)
	w := &models.Worker{
		Name:          "worker-stale",
		RuntimeType:   models.WorkerRuntimeTypePodman,
		Status:        models.WorkerStatusReady,
		LastHeartbeat: &past,
	}
	if err := repo.Upsert(context.Background(), w); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Sweep with a 1-minute timeout — the worker is 2 minutes stale.
	reg.SweepStale(context.Background(), time.Minute)

	workers, _ := repo.GetAll(context.Background())
	if workers[0].Status != models.WorkerStatusDisconnected {
		t.Errorf("expected status %q, got %q", models.WorkerStatusDisconnected, workers[0].Status)
	}
}
