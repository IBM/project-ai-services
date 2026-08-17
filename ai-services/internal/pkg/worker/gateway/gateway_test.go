package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20 // 1 MiB

// ──────────────────────────────────────────────────────────────────────────────
// Minimal in-memory WorkerRepository used by gateway tests (no DB needed).
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
// Test helpers
// ──────────────────────────────────────────────────────────────────────────────

// preregister calls registry.Preregister and returns the bootstrap token,
// failing the test on any error.
func preregister(t *testing.T, reg *registry.Registry, workerName string) string {
	t.Helper()
	token, err := reg.Preregister(context.Background(), workerName)
	if err != nil {
		t.Fatalf("Preregister(%q): %v", workerName, err)
	}
	return token
}

// startTestGateway creates an in-process gRPC server backed by bufconn and
// returns a connected client plus a stop func.
func startTestGateway(t *testing.T, reg *registry.Registry) (workerpb.WorkerGatewayClient, func()) {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	gw := New(reg)
	gw.grpcServer = grpc.NewServer()
	workerpb.RegisterWorkerGatewayServer(gw.grpcServer, gw)

	go gw.grpcServer.Serve(lis) //nolint:errcheck

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	stop := func() {
		conn.Close()        //nolint:errcheck
		gw.grpcServer.Stop()
		lis.Close() //nolint:errcheck
	}

	return workerpb.NewWorkerGatewayClient(conn), stop
}

// ──────────────────────────────────────────────────────────────────────────────
// Register RPC
// ──────────────────────────────────────────────────────────────────────────────

func TestGateway_Register_ValidToken(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())
	token := preregister(t, reg, "worker-1")

	client, stop := startTestGateway(t, reg)
	defer stop()

	resp, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	})
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if resp.GetWorkerName() != "worker-1" {
		t.Errorf("expected WorkerName %q, got %q", "worker-1", resp.GetWorkerName())
	}

	// Worker must now appear in the in-memory registry.
	if _, ok := reg.Get("worker-1"); !ok {
		t.Error("expected worker-1 to be in registry after Register")
	}
}

func TestGateway_Register_InvalidToken(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	_, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: "bad-token",
	})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestGateway_Register_TokenSingleUse(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())
	token := preregister(t, reg, "worker-1")

	client, stop := startTestGateway(t, reg)
	defer stop()

	// First use succeeds.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	}); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}

	// Second use of the same token must fail.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	}); err == nil {
		t.Fatal("second Register: expected error for reused token")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CommandStream RPC
// ──────────────────────────────────────────────────────────────────────────────

func TestGateway_CommandStream_UnregisteredWorker(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	stream, err := client.CommandStream(context.Background())
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Identify as an unknown worker — should receive Unauthenticated.
	if err := stream.Send(&workerpb.CommandResult{WorkerName: "nobody"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for unregistered worker")
	}
}

func TestGateway_CommandStream_MissingWorkerName(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())

	client, stop := startTestGateway(t, reg)
	defer stop()

	stream, err := client.CommandStream(context.Background())
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Send a message with no worker_name — should receive InvalidArgument.
	if err := stream.Send(&workerpb.CommandResult{WorkerName: ""}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for missing worker_name")
	}
}

func TestGateway_CommandStream_CommandDelivered(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-2")

	client, stop := startTestGateway(t, reg)
	defer stop()

	// Register the worker via the RPC.
	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// First message identifies the worker (heartbeat).
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-2",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	// Give the gateway a moment to register the stream.
	time.Sleep(20 * time.Millisecond)

	// Enqueue a command directly onto the worker's channel.
	entry, ok := reg.Get("worker-2")
	if !ok {
		t.Fatal("expected worker-2 in registry after Register RPC")
	}
	entry.CommandCh <- &workerpb.Command{
		CommandId: "test-cmd-1",
		Type:      workerpb.CommandType_COMMAND_TYPE_LIST_PODS,
	}

	// The stream must deliver the command to the worker client.
	got, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv command: %v", err)
	}
	if got.GetCommandId() != "test-cmd-1" {
		t.Errorf("expected command_id %q, got %q", "test-cmd-1", got.GetCommandId())
	}
}

func TestGateway_CommandStream_ResultRouted(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-3")

	client, stop := startTestGateway(t, reg)
	defer stop()

	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// First message: identify worker.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-3",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// Register a waiter before the worker sends back a result.
	resultCh, err := reg.WaitForResult("worker-3", "cmd-xyz")
	if err != nil {
		t.Fatalf("WaitForResult: %v", err)
	}

	// Worker sends the result.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName: "worker-3",
		CommandId:  "cmd-xyz",
		Success:    true,
	}); err != nil {
		t.Fatalf("Send result: %v", err)
	}

	select {
	case res := <-resultCh:
		if res.GetCommandId() != "cmd-xyz" {
			t.Errorf("expected command_id %q, got %q", "cmd-xyz", res.GetCommandId())
		}
		if !res.GetSuccess() {
			t.Error("expected success=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result to be routed")
	}
}

func TestGateway_CommandStream_Disconnect(t *testing.T) {
	repo := newFakeWorkerRepo()
	reg := registry.New(repo)
	token := preregister(t, reg, "worker-4")

	client, stop := startTestGateway(t, reg)
	defer stop()

	if _, err := client.Register(context.Background(), &workerpb.RegisterRequest{
		PreSharedToken: token,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := client.CommandStream(ctx)
	if err != nil {
		t.Fatalf("CommandStream: %v", err)
	}

	// Identify the worker.
	if err := stream.Send(&workerpb.CommandResult{
		WorkerName:  "worker-4",
		IsHeartbeat: true,
	}); err != nil {
		t.Fatalf("Send identify: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if _, ok := reg.Get("worker-4"); !ok {
		t.Fatal("expected worker-4 in registry after opening stream")
	}

	// Cancel the client-side context to simulate the worker disconnecting.
	cancel()

	// Give the gateway time to process the disconnect.
	time.Sleep(50 * time.Millisecond)

	if _, ok := reg.Get("worker-4"); ok {
		t.Error("expected worker-4 to be removed from registry after disconnect")
	}
}
