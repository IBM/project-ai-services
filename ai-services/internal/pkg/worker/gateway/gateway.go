// Package gateway implements the WorkerGateway gRPC server.
// It is co-located with the Catalog API Server (control plane) and listens
// on a separate port (default :9090) for bidirectional streams from worker
// daemons.
package gateway

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"google.golang.org/grpc"
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
	tokenStore *registry.TokenStore
	repo       repository.WorkerRepository // may be nil in tests
	grpcServer *grpc.Server
}

// New creates a Gateway backed by the given registry, token store, and worker repository.
func New(reg *registry.Registry, ts *registry.TokenStore, repo repository.WorkerRepository) *Gateway {
	return &Gateway{
		registry:   reg,
		tokenStore: ts,
		repo:       repo,
	}
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

// runSweeper periodically queries the DB for workers whose last_heartbeat has
// exceeded heartbeatTimeout and marks them disconnected.
// It runs entirely against the DB — no in-memory state required.
func (g *Gateway) runSweeper(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.sweepStaleWorkers(ctx)
		}
	}
}

func (g *Gateway) sweepStaleWorkers(ctx context.Context) {
	if g.repo == nil {
		return
	}

	workers, err := g.repo.GetAll(ctx)
	if err != nil {
		logger.WarningfCtx(ctx, "WorkerGateway sweeper: failed to fetch workers: %v", err)

		return
	}

	status := models.WorkerStatusDisconnected
	now := time.Now()

	for _, w := range workers {
		if w.Status == models.WorkerStatusDisconnected {
			continue
		}
		if w.LastHeartbeat == nil || now.Sub(*w.LastHeartbeat) > heartbeatTimeout {
			logger.WarningfCtx(ctx, "WorkerGateway sweeper: worker %s heartbeat timed out — marking disconnected", w.Name)
			if err := g.repo.Update(ctx, w.Name, repository.WorkerUpdate{Status: &status}); err != nil {
				logger.WarningfCtx(ctx, "WorkerGateway sweeper: failed to update worker %s: %v", w.Name, err)
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// WorkerGatewayServer implementation
// ──────────────────────────────────────────────────────────────────────────────

// Register implements WorkerGatewayServer. Workers call this once at bootstrap.
// Metadata supplied in the request is persisted to the
// DB metadata JSON column by registry.Register — no separate handling needed here.
func (g *Gateway) Register(ctx context.Context, req *workerpb.RegisterRequest) (*workerpb.RegisterResponse, error) {
	workerName := req.GetWorkerName()
	logger.InfofCtx(ctx, "WorkerGateway: Register request from worker_name=%s", workerName)

	if err := g.tokenStore.Validate(req.GetPreSharedToken()); err != nil {
		logger.WarningfCtx(ctx, "WorkerGateway: rejected registration for worker %s: %v", workerName, err)

		return nil, fmt.Errorf("registration rejected: %w", err)
	}

	if _, err := g.registry.Register(ctx, req); err != nil {
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
func (g *Gateway) identifyWorker(ctx context.Context, stream grpc.BidiStreamingServer[workerpb.CommandResult, workerpb.Command]) (string, *registry.WorkerEntry, error) {
	firstMsg, err := stream.Recv()
	if err != nil {
		return "", nil, fmt.Errorf("CommandStream: failed to receive first message: %w", err)
	}
	workerName := firstMsg.GetWorkerName()
	if workerName == "" {
		return "", nil, fmt.Errorf("CommandStream: first message missing worker name")
	}

	entry, ok := g.registry.Get(workerName)
	if !ok {
		return "", nil, fmt.Errorf("CommandStream: unknown worker %s – call Register first", workerName)
	}

	logger.InfofCtx(ctx, "WorkerGateway: CommandStream opened for worker %s", workerName)

	// Deliver the first message if it is a real result (not a heartbeat).
	if firstMsg.GetIsHeartbeat() {
		g.updateHeartbeat(ctx, workerName)
	} else {
		g.registry.DeliverResult(firstMsg)
	}

	return workerName, entry, nil
}

// recvLoop reads CommandResults from the stream and dispatches them to waiting callers.
// Heartbeat messages update last_heartbeat in the DB.
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
			g.updateHeartbeat(ctx, workerName)

			continue
		}

		g.registry.DeliverResult(res)
	}
}

// updateHeartbeat writes the current timestamp to last_heartbeat in the DB.
func (g *Gateway) updateHeartbeat(ctx context.Context, workerName string) {
	if g.repo == nil {
		return
	}
	now := time.Now()
	if err := g.repo.Update(ctx, workerName, repository.WorkerUpdate{LastHeartbeat: &now}); err != nil {
		logger.WarningfCtx(ctx, "WorkerGateway: heartbeat update failed for %s: %v", workerName, err)
	}
}
