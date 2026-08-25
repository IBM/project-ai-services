// Package gateway implements the WorkerGateway gRPC server.
// It is co-located with the Catalog API Server (control plane) and listens
// on a separate port (default :9090) for bidirectional streams from worker
// daemons.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// heartbeatTimeout is the maximum time since the last recorded heartbeat
	// before a worker is considered disconnected.
	heartbeatTimeout = 90 * time.Second

	// sweepInterval is how often the background sweeper checks for stale workers.
	sweepInterval = 30 * time.Second
)

// Gateway is the gRPC server that accepts connections from workers.
type Gateway struct {
	workerpb.UnimplementedWorkerGatewayServer

	registry   *registry.Registry
	grpcServer *grpc.Server
}

// New creates a Gateway backed by the given registry.
func New(reg *registry.Registry) *Gateway {
	return &Gateway{registry: reg}
}

// Start begins listening on addr (e.g. ":9090") and serves gRPC in a background goroutine.
// It also starts the heartbeat sweeper. Both stop when ctx is cancelled.
// cancel is a CancelCauseFunc for the server's root context; it is called with the
// Serve error if the gRPC listener fails unexpectedly, so the whole process shuts down cleanly.
func (g *Gateway) Start(ctx context.Context, cancel context.CancelCauseFunc, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("worker gateway: listen on %s: %w", addr, err)
	}

	g.grpcServer = grpc.NewServer()
	workerpb.RegisterWorkerGatewayServer(g.grpcServer, g)

	go func() {
		logger.InfofCtx(ctx, "WorkerGateway gRPC server listening on %s", addr)
		if err := g.grpcServer.Serve(lis); err != nil {
			// Serve returned an unexpected error (not a clean GracefulStop).
			// Cancel the root context so the whole server shuts down and the
			// process exits with a non-zero code, triggering a pod restart.
			logger.ErrorfCtx(ctx, "WorkerGateway gRPC server failed: %v", err)
			cancel(fmt.Errorf("worker gateway: gRPC server failed: %w", err))
		}
	}()

	go g.runSweeper(ctx)

	go func() {
		<-ctx.Done()
		logger.InfolnCtx(ctx, "WorkerGateway shutting down")
		g.grpcServer.GracefulStop()
	}()

	return nil
}

// runSweeper periodically asks the registry to mark stale workers disconnected.
func (g *Gateway) runSweeper(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.registry.SweepStale(ctx, heartbeatTimeout)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// WorkerGatewayServer implementation
// ──────────────────────────────────────────────────────────────────────────────

// Register implements WorkerGatewayServer. Workers call this once at bootstrap.
// The worker name is taken from the token, not from the request — the worker
// cannot self-assign a name different from what was pre-registered by an admin.
// Metadata supplied in the request is persisted to the DB metadata JSON column.
func (g *Gateway) Register(ctx context.Context, req *workerpb.RegisterRequest) (*workerpb.RegisterResponse, error) {
	logger.InfofCtx(ctx, "WorkerGateway: Register request received")

	// Validate token and recover the pre-registered worker name.
	workerName, err := g.registry.ValidateToken(req.GetPreSharedToken())
	if err != nil {
		logger.WarningfCtx(ctx, "WorkerGateway: rejected registration: %v", err)

		return nil, fmt.Errorf("registration rejected: %w", err)
	}

	if _, err := g.registry.Register(ctx, workerName, req.GetRuntimeType(), req.GetMetadata()); err != nil {
		if errors.Is(err, registry.ErrWorkerAlreadyActive) {
			return nil, status.Errorf(codes.AlreadyExists, "worker %s is already active", workerName)
		}

		return nil, fmt.Errorf("failed to register worker: %w", err)
	}

	logger.InfofCtx(ctx, "WorkerGateway: worker %s registered", workerName)

	return &workerpb.RegisterResponse{
		WorkerName: workerName,
		// TlsCertPem / TlsKeyPem intentionally empty; mTLS added in a future iteration.
	}, nil
}

// CommandStream implements WorkerGatewayServer.
// The worker initiates the bidirectional stream. This method:
//  1. Reads the first CommandResult to identify which worker connected.
//  2. Routes incoming results to the waiting RemoteRuntime callers.
//  3. Drains the worker's CommandCh and writes Commands to the stream.
func (g *Gateway) CommandStream(stream grpc.BidiStreamingServer[workerpb.CommandResult, workerpb.Command]) error { //nolint:gocognit
	ctx := stream.Context()

	workerName, entry, err := g.identifyWorker(ctx, stream)
	if err != nil {
		return err
	}

	// goroutine: read results from the worker and dispatch to waiting callers.
	recvErrCh := make(chan error, 1)
	go g.recvLoop(ctx, stream, workerName, recvErrCh)

	// Main loop: pull Commands from the worker's CommandCh and send them downstream.
	for {
		select {
		case <-ctx.Done():
			g.registry.Disconnect(context.Background(), workerName)
			logger.InfofCtx(ctx, "WorkerGateway: context done for worker %s", workerName)

			return ctx.Err()

		case err := <-recvErrCh:
			g.registry.Disconnect(context.Background(), workerName)
			logger.InfofCtx(ctx, "WorkerGateway: worker %s disconnected: %v", workerName, err)

			return err

		case cmd, ok := <-entry.CommandCh:
			if !ok {
				return fmt.Errorf("CommandStream: command channel closed for worker %s", workerName)
			}
			if err := stream.Send(cmd); err != nil {
				g.registry.Disconnect(context.Background(), workerName)

				return fmt.Errorf("CommandStream: send to worker %s: %w", workerName, err)
			}
		}
	}
}

// identifyWorker reads the first message from the stream, validates the worker is known,
// and returns the worker name and registry entry.
//
// Error codes used by the worker daemon to decide its retry strategy:
//   - codes.Unauthenticated — worker not in registry; must call Register before retrying CommandStream.
//   - codes.InvalidArgument  — first message is malformed; worker has a bug.
//   - any other error        — transient; retry CommandStream with backoff (no re-registration needed).
func (g *Gateway) identifyWorker(ctx context.Context, stream grpc.BidiStreamingServer[workerpb.CommandResult, workerpb.Command]) (string, *registry.WorkerEntry, error) {
	firstMsg, err := stream.Recv()
	if err != nil {
		return "", nil, fmt.Errorf("CommandStream: failed to receive first message: %w", err)
	}
	workerName := firstMsg.GetWorkerName()
	if workerName == "" {
		return "", nil, status.Error(codes.InvalidArgument, "CommandStream: first message missing worker_name")
	}

	entry, ok := g.registry.Get(workerName)
	if !ok {
		// The control plane has no in-memory entry for this worker — either it never
		// registered or the control plane restarted and lost its registry.
		// Return Unauthenticated so the worker knows it must call Register again
		// before opening a new CommandStream.
		return "", nil, status.Errorf(codes.Unauthenticated,
			"CommandStream: worker %s not registered — call Register first", workerName)
	}

	logger.InfofCtx(ctx, "WorkerGateway: CommandStream opened for worker %s", workerName)

	// Deliver the first message if it is a real result (not a heartbeat).
	if firstMsg.GetIsHeartbeat() {
		g.registry.UpdateHeartbeat(ctx, workerName)
	} else {
		g.registry.DeliverResult(firstMsg)
	}

	return workerName, entry, nil
}

// recvLoop reads CommandResults from the stream and dispatches them to waiting callers.
// Heartbeat messages update last_heartbeat in the DB via the registry.
// Errors are sent to errCh and the goroutine exits.
func (g *Gateway) recvLoop(ctx context.Context, stream grpc.BidiStreamingServer[workerpb.CommandResult, workerpb.Command], workerName string, errCh chan<- error) {
	for {
		res, err := stream.Recv()
		if err != nil {
			errCh <- err

			return
		}
		if res.WorkerName == "" {
			res.WorkerName = workerName
		}

		if res.GetIsHeartbeat() {
			g.registry.UpdateHeartbeat(ctx, workerName)

			continue
		}

		g.registry.DeliverResult(res)
	}
}
