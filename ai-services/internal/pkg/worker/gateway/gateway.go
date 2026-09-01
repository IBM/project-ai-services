// Package gateway implements the WorkerGateway gRPC server.
// It is co-located with the Catalog API Server (control plane) and listens
// on a separate port (default :9090) for bidirectional streams from worker
// daemons.
package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
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

	// PKI material — loaded or generated from pkiDir on first start (see pki.go).
	caCert     *x509.Certificate
	caKey      *ecdsa.PrivateKey
	serverCert tls.Certificate
	caCertPool *x509.CertPool
}

// New creates a Gateway backed by the given registry.
func New(ctx context.Context, reg *registry.Registry) (*Gateway, error) {
	pki, err := loadOrGeneratePKI(ctx, workerconstants.GatewayPKIDir)
	if err != nil {
		return nil, fmt.Errorf("worker gateway: PKI init failed: %w", err)
	}

	return &Gateway{
		registry:   reg,
		caCert:     pki.caCert,
		caKey:      pki.caKey,
		serverCert: pki.serverCert,
		caCertPool: pki.caCertPool,
	}, nil
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

	// Hybrid TLS: allow connections without client certs (for bootstrap Register)
	// but verify them rigorously if they are provided (for mTLS CommandStream).
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{g.serverCert},
		ClientCAs:    g.caCertPool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}

	g.grpcServer = grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.UnaryInterceptor(g.authUnaryInterceptor),
		grpc.StreamInterceptor(g.authStreamInterceptor),
	)
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
	// 1. Validate token — worker name is bound to the token, not the request.
	workerName, err := g.registry.ValidateToken(req.GetPreSharedToken())
	if err != nil {
		logger.WarningfCtx(ctx, "WorkerGateway: rejected registration: %v", err)

		return nil, status.Errorf(codes.Unauthenticated, "registration rejected: %v", err)
	}

	// 2. Parse, validate, and sign the CSR (required for mTLS). See pki.go: signWorkerCSR.
	csrPEM := req.GetCsrPem()
	if len(csrPEM) == 0 {
		logger.ErrorfCtx(ctx, "WorkerGateway: registration rejected for worker=%s: CSR is required", workerName)

		return nil, status.Errorf(codes.InvalidArgument, "CSR is required")
	}

	tlsCertPEM, caCertPEM, notAfter, signErr := signWorkerCSR(csrPEM, workerName, g.caCert, g.caKey)
	if signErr != nil {
		logger.WarningfCtx(ctx, "WorkerGateway: CSR error for worker=%s: %v", workerName, signErr)

		return nil, status.Errorf(codes.InvalidArgument, "CSR rejected: %v", signErr)
	}

	// 3. Register in-memory and persist to DB.
	if _, err := g.registry.Register(ctx, workerName, req.GetRuntimeType(), req.GetMetadata()); err != nil {
		if errors.Is(err, registry.ErrWorkerAlreadyActive) {
			return nil, status.Errorf(codes.AlreadyExists, "worker %s is already active", workerName)
		}

		return nil, fmt.Errorf("failed to register worker: %w", err)
	}

	logger.InfofCtx(ctx, "WorkerGateway: worker %q registered, cert valid until %s",
		workerName, notAfter.UTC().Format("2006-01-02"))

	return &workerpb.RegisterResponse{
		TlsCertPem: tlsCertPEM,
		CaCertPem:  caCertPEM,
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
// On a registry miss, identifyWorker checks the DB using the worker's verified mTLS
// client certificate as proof of identity and re-populates the in-memory entry if the
// worker was previously registered (status ready or disconnected). This covers two
// cases transparently:
//   - Control-plane restart: registry is empty but DB rows survive.
//   - Worker reconnect after a clean disconnect: DB status is disconnected.
//
// Error codes used by the worker daemon to decide its retry strategy:
//   - codes.Unauthenticated — worker not in DB or status pending; must call Register
//     (with a new token) before retrying CommandStream.
//   - codes.InvalidArgument  — first message is malformed; worker has a bug.
//   - any other error        — transient; retry CommandStream with backoff.
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
		// No in-memory entry — either the control plane restarted or the worker is
		// reconnecting after a clean disconnect. Attempt to restore the entry from
		// the DB. The authStreamInterceptor already verified the cert chain via
		// enforceClientCert, and the worker name in the first message is derived from
		// the cert CN on the worker side, so it is trustworthy.
		restored, restoreErr := g.registry.Restore(ctx, workerName)
		if restoreErr != nil {
			if errors.Is(restoreErr, registry.ErrWorkerNotFound) {
				return "", nil, status.Errorf(codes.Unauthenticated,
					"CommandStream: worker %s not registered — call Register first", workerName)
			}

			return "", nil, fmt.Errorf("CommandStream: restore registry entry for %s: %w", workerName, restoreErr)
		}

		logger.InfofCtx(ctx, "WorkerGateway: restored registry entry for worker %s (DB status was %s)",
			workerName, "disconnected or ready")
		entry = restored
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
