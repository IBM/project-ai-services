package registry

import (
	"context"
	"errors"
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

func (r *fakeWorkerRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Worker, error) {
	w, ok := r.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *fakeWorkerRepo) GetByName(_ context.Context, name string) (*models.Worker, error) {
	w, ok := r.workers[name]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (r *fakeWorkerRepo) GetApplicationIDsByWorkerIDs(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
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

	_, err := reg.Register(context.Background(), "worker-1", "docker", nil)
	if err == nil {
		t.Fatal("expected error for unsupported runtime_type")
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

// ──────────────────────────────────────────────────────────────────────────────
// IsWorkerConnected tests
// ──────────────────────────────────────────────────────────────────────────────

func TestRegistry_IsWorkerConnected_NotInCache(t *testing.T) {
	reg := New(nil)

	if reg.IsWorkerConnected(context.Background(), "ghost") {
		t.Error("expected false for worker not in cache")
	}
}

func TestRegistry_IsWorkerConnected_CacheHitNoRepo(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	if !reg.IsWorkerConnected(context.Background(), "worker-1") {
		t.Error("expected true: worker is in cache and no repo to check")
	}
}

func TestRegistry_IsWorkerConnected_CacheHitDBReady(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	if _, err := reg.Preregister(context.Background(), "worker-1"); err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	if _, err := reg.Register(context.Background(), "worker-1", "podman", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// DB row has status=ready after Register.
	if !reg.IsWorkerConnected(context.Background(), "worker-1") {
		t.Error("expected true: in cache and DB status=ready")
	}
}

func TestRegistry_IsWorkerConnected_CacheHitDBNotReady(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	if _, err := reg.Preregister(context.Background(), "worker-1"); err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	if _, err := reg.Register(context.Background(), "worker-1", "podman", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Force DB status to disconnected.
	w := repo.workers["worker-1"]
	disc := models.WorkerStatusDisconnected
	if err := repo.Update(context.Background(), w.ID, repository.WorkerUpdate{Status: &disc}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if reg.IsWorkerConnected(context.Background(), "worker-1") {
		t.Error("expected false: DB status is disconnected")
	}
}

func TestRegistry_IsWorkerConnected_DisconnectedRemovedFromCache(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck
	reg.Disconnect(context.Background(), "worker-1")

	if reg.IsWorkerConnected(context.Background(), "worker-1") {
		t.Error("expected false: worker was disconnected")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// WorkerRuntimeType / WorkerMetadata / WorkerCommandChannel tests
// ──────────────────────────────────────────────────────────────────────────────

func TestRegistry_WorkerRuntimeType_Connected(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	rt, ok := reg.WorkerRuntimeType("worker-1")
	if !ok {
		t.Fatal("expected ok=true for connected worker")
	}
	if rt != "podman" {
		t.Errorf("expected runtime type %q, got %q", "podman", rt)
	}
}

func TestRegistry_WorkerRuntimeType_NotConnected(t *testing.T) {
	reg := New(nil)

	_, ok := reg.WorkerRuntimeType("ghost")
	if ok {
		t.Error("expected ok=false for unknown worker")
	}
}

func TestRegistry_WorkerMetadata_Connected(t *testing.T) {
	reg := New(nil)
	meta := map[string]string{"domainSuffix": "example.com", "httpsPort": "443"}
	reg.Register(context.Background(), "worker-1", "podman", meta) //nolint:errcheck

	got, ok := reg.WorkerMetadata("worker-1")
	if !ok {
		t.Fatal("expected ok=true for connected worker")
	}
	if got["domainSuffix"] != "example.com" {
		t.Errorf("expected domainSuffix %q, got %q", "example.com", got["domainSuffix"])
	}
	if got["httpsPort"] != "443" {
		t.Errorf("expected httpsPort %q, got %q", "443", got["httpsPort"])
	}
}

func TestRegistry_WorkerMetadata_NotConnected(t *testing.T) {
	reg := New(nil)

	_, ok := reg.WorkerMetadata("ghost")
	if ok {
		t.Error("expected ok=false for unknown worker")
	}
}

func TestRegistry_WorkerCommandChannel_Connected(t *testing.T) {
	reg := New(nil)
	reg.Register(context.Background(), "worker-1", "podman", nil) //nolint:errcheck

	ch, ok := reg.WorkerCommandChannel("worker-1")
	if !ok {
		t.Fatal("expected ok=true for connected worker")
	}
	if ch == nil {
		t.Error("expected non-nil command channel")
	}
}

func TestRegistry_WorkerCommandChannel_NotConnected(t *testing.T) {
	reg := New(nil)

	ch, ok := reg.WorkerCommandChannel("ghost")
	if ok {
		t.Error("expected ok=false for unknown worker")
	}
	if ch != nil {
		t.Error("expected nil channel for unknown worker")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Restore tests
// ──────────────────────────────────────────────────────────────────────────────

// TestRegistry_Restore_AfterRestartReturnsEntry simulates a control-plane restart:
// a worker was previously registered (DB row exists with status=ready), the
// in-memory registry is empty, and Restore should recreate the entry.
func TestRegistry_Restore_AfterRestartReturnsEntry(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	id := uuid.New()
	repo.workers["worker-r1"] = &models.Worker{
		ID:          id,
		Name:        "worker-r1",
		RuntimeType: models.WorkerRuntimeTypePodman,
		Status:      models.WorkerStatusReady,
		Metadata:    map[string]any{"base_dir": "/data"},
	}
	repo.byID[id] = repo.workers["worker-r1"]

	if _, ok := reg.Get("worker-r1"); ok {
		t.Fatal("expected worker-r1 not in memory before Restore")
	}

	entry, err := reg.Restore(context.Background(), "worker-r1")
	if err != nil {
		t.Fatalf("Restore: unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("Restore: expected non-nil entry")
	}
	if entry.WorkerName != "worker-r1" {
		t.Errorf("expected WorkerName %q, got %q", "worker-r1", entry.WorkerName)
	}
	if entry.CommandCh == nil {
		t.Error("expected non-nil CommandCh after Restore")
	}
	if entry.Metadata["base_dir"] != "/data" {
		t.Errorf("expected metadata base_dir=/data, got %q", entry.Metadata["base_dir"])
	}
	if _, ok := reg.Get("worker-r1"); !ok {
		t.Error("expected worker-r1 in registry after Restore")
	}
	if repo.workers["worker-r1"].Status != models.WorkerStatusReady {
		t.Errorf("expected DB status ready after Restore, got %q", repo.workers["worker-r1"].Status)
	}
}

// TestRegistry_Restore_DisconnectedWorkerReturnsEntry simulates a worker reconnecting
// after a clean disconnect: DB status is disconnected, registry is empty.
func TestRegistry_Restore_DisconnectedWorkerReturnsEntry(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	id := uuid.New()
	repo.workers["worker-dc"] = &models.Worker{
		ID:          id,
		Name:        "worker-dc",
		RuntimeType: models.WorkerRuntimeTypePodman,
		Status:      models.WorkerStatusDisconnected,
	}
	repo.byID[id] = repo.workers["worker-dc"]

	entry, err := reg.Restore(context.Background(), "worker-dc")
	if err != nil {
		t.Fatalf("Restore: unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry for disconnected worker")
	}
	// DB must be updated to ready.
	if repo.workers["worker-dc"].Status != models.WorkerStatusReady {
		t.Errorf("expected DB status ready after Restore, got %q", repo.workers["worker-dc"].Status)
	}
}

// TestRegistry_Restore_PendingWorkerReturnsNotFound ensures a worker that was
// pre-registered but never completed the bootstrap flow cannot be restored via a
// cert alone — it must go through the full Register token flow.
func TestRegistry_Restore_PendingWorkerReturnsNotFound(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	id := uuid.New()
	repo.workers["worker-pending"] = &models.Worker{
		ID:     id,
		Name:   "worker-pending",
		Status: models.WorkerStatusPending,
	}
	repo.byID[id] = repo.workers["worker-pending"]

	_, err := reg.Restore(context.Background(), "worker-pending")
	if err == nil {
		t.Fatal("expected error for pending worker")
	}
	if !errors.Is(err, ErrWorkerNotFound) {
		t.Errorf("expected ErrWorkerNotFound, got %v", err)
	}
}

// TestRegistry_Restore_UnknownWorkerReturnsNotFound ensures a worker with no DB
// row at all is correctly rejected.
func TestRegistry_Restore_UnknownWorkerReturnsNotFound(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	_, err := reg.Restore(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error for unknown worker")
	}
	if !errors.Is(err, ErrWorkerNotFound) {
		t.Errorf("expected ErrWorkerNotFound, got %v", err)
	}
}

// TestRegistry_Restore_NoRepoReturnsNotFound covers the nil-repo (test-only) case.
func TestRegistry_Restore_NoRepoReturnsNotFound(t *testing.T) {
	reg := New(nil)

	_, err := reg.Restore(context.Background(), "worker-x")
	if err == nil {
		t.Fatal("expected error when no repo configured")
	}
	if !errors.Is(err, ErrWorkerNotFound) {
		t.Errorf("expected ErrWorkerNotFound, got %v", err)
	}
}

// TestRegistry_Restore_Idempotent verifies that calling Restore twice returns
// the same entry without creating a duplicate in-memory entry.
func TestRegistry_Restore_Idempotent(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := New(repo)

	id := uuid.New()
	repo.workers["worker-idem"] = &models.Worker{
		ID:          id,
		Name:        "worker-idem",
		RuntimeType: models.WorkerRuntimeTypePodman,
		Status:      models.WorkerStatusReady,
	}
	repo.byID[id] = repo.workers["worker-idem"]

	e1, err := reg.Restore(context.Background(), "worker-idem")
	if err != nil {
		t.Fatalf("first Restore: %v", err)
	}
	e2, err := reg.Restore(context.Background(), "worker-idem")
	if err != nil {
		t.Fatalf("second Restore: %v", err)
	}
	if e1 != e2 {
		t.Error("expected both Restore calls to return the same WorkerEntry pointer")
	}
}
