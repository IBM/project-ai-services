// Package join implements the worker join workflow.
//
// The join flow consists of three steps:
//
//  1. Setup — Deploy the Caddy reverse-proxy pod on the worker node so the
//     worker can serve proxied routes once it is connected.
//
//  2. Register — Dial the catalog gRPC worker-gateway and call Register once,
//     presenting the single-use bootstrap token obtained from
//     `ai-services catalog worker register`.  The control plane validates the
//     token, binds the worker name, and acknowledges registration.
//
//  3. Connect — Open the long-lived CommandStream bidirectional gRPC stream and
//     maintain it, forwarding heartbeats to the control plane so it knows the
//     worker is alive.  The stream is retried with exponential back-off on
//     transient failures.  If the control plane signals Unauthenticated the
//     worker must call Register again before reconnecting.
package join

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerdeploy "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/dispatch"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

const (
	// heartbeatInterval is how often the worker sends a keep-alive to the control plane.
	heartbeatInterval = 30 * time.Second

	// retryBase is the initial back-off duration before retrying CommandStream.
	retryBase = 5 * time.Second
	// retryMax caps the back-off so the worker does not wait too long after
	// a prolonged outage on the control-plane side.
	retryMax = 2 * time.Minute

	// retryBackoffFactor is the exponential multiplier applied to the backoff duration.
	retryBackoffFactor = 2
)

// Options carries everything needed to join a worker to the catalog control plane.
type Options struct {
	// GatewayAddr is the host:port of the catalog gRPC worker-gateway,
	// e.g. "catalog.example.com:9090".
	GatewayAddr string

	// Token is the single-use bootstrap token issued by
	// `ai-services catalog worker register`.
	Token string

	// RuntimeType is the execution environment of this worker node
	// ("podman" or "openshift"). Sent to the control plane during Register.
	RuntimeType types.RuntimeType

	// Setup holds the options for setting up this worker node (Caddy proxy,
	// model storage, etc.). Setup runs before the gRPC handshake so the
	// worker is ready to serve routes as soon as it connects.
	Setup workerdeploy.Options
}

// Run executes the complete worker join workflow and blocks until ctx is
// cancelled or an unrecoverable error occurs.
//
// The steps are:
//   - Deploy Caddy on the worker node (idempotent).
//   - Dial the catalog gRPC gateway.
//   - Call Register with the bootstrap token.
//   - Open CommandStream and hold it, retrying on transient failures.
func Run(ctx context.Context, opts Options) error {
	switch opts.RuntimeType {
	case types.RuntimeTypePodman:
		rt, err := runtime.CreateRuntime(opts.RuntimeType, "")
		if err != nil {
			return fmt.Errorf("worker join: init runtime: %w", err)
		}

		// Setup worker node
		if err := workerdeploy.Setup(ctx, rt, opts.Setup, opts.GatewayAddr, opts.Token); err != nil {
			return fmt.Errorf("worker join: setup: %w", err)
		}
	case types.RuntimeTypeOpenShift:
		rt, err := runtime.CreateRuntime(opts.RuntimeType, workerconstants.WorkerAppName)
		if err != nil {
			return fmt.Errorf("worker join: init runtime: %w", err)
		}
		

		fmt.Println("openshift")
	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.RuntimeType)
	}

	return nil
}

// GrpcServer dials the catalog gRPC worker-gateway, registers with the
// bootstrap token, and holds the CommandStream open.
func GrpcServer(ctx context.Context, opts Options) error {
	domainSuffix, err := utils.ComputeDomainSuffix(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath, opts.Setup.DomainName)
	if err != nil {
		return err
	}

	rt, err := runtime.CreateRuntime(opts.RuntimeType, "")
	if err != nil {
		return fmt.Errorf("worker grpcserver: init runtime: %w", err)
	}

	// ── Step 1: Build Caddy proxy router (Podman only) ──────────────────────
	// Must happen after Setup so the Caddy pod is running and its admin port
	// is discoverable. For OpenShift workers routes are managed natively.
	var pr *workercaddy.ProxyRouter
	if opts.RuntimeType == types.RuntimeTypePodman {
		var err error
		if pr, err = workercaddy.New(ctx, rt); err != nil {
			return fmt.Errorf("worker grpcserver: init local Caddy manager: %w", err)
		}
	}

	// ── Step 2: Dial the gateway ─────────────────────────────────────────────
	logger.InfofCtx(ctx, "Connecting to catalog gateway at %s...\n", opts.GatewayAddr)

	conn, err := grpc.NewClient(opts.GatewayAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("worker grpcserver: create client for %s: %w", opts.GatewayAddr, err)
	}
	defer func() { _ = conn.Close() }()

	client := workerpb.NewWorkerGatewayClient(conn)

	// ── Step 3: Register + stream loop ───────────────────────────────────────
	meta := map[string]string{
		workerconstants.MetaKeyBaseDir:      opts.Setup.BaseDir,
		workerconstants.MetaKeyDomainSuffix: domainSuffix,
		workerconstants.MetaKeyHTTPSPort:    strconv.Itoa(opts.Setup.HTTPSPort),
	}

	return runRegistrationLoop(ctx, rt, pr, client, opts.Token, meta)
}

// ─── registration loop ────────────────────────────────────────────────────────

// runRegistrationLoop calls Register and then enters the CommandStream retry
// loop.  If the stream comes back with codes.Unauthenticated it re-registers
// before reconnecting.
func runRegistrationLoop(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, client workerpb.WorkerGatewayClient, token string, meta map[string]string) error {
	workerName, err := register(ctx, client, token, rt.Type(), meta)
	if err != nil {
		return fmt.Errorf("worker join: register: %w", err)
	}

	logger.InfofCtx(ctx, "Worker %q registered with control plane.\n", workerName)

	return runStreamLoop(ctx, rt, pr, client, workerName)
}

// register calls the Register RPC once and returns the worker name bound by
// the control plane.
func register(ctx context.Context, client workerpb.WorkerGatewayClient, token string, rt types.RuntimeType, meta map[string]string) (string, error) {
	logger.InfolnCtx(ctx, "Registering worker with catalog control plane...")

	resp, err := client.Register(ctx, &workerpb.RegisterRequest{
		PreSharedToken: token,
		RuntimeType:    rt.String(),
		Metadata:       meta,
	})
	// TODO: When registrations fails due to "token already used" error
	// attempt registretion without token.
	if err != nil {
		return "", fmt.Errorf("register RPC: %w", err)
	}

	return resp.GetWorkerName(), nil
}

// ─── command-stream loop ──────────────────────────────────────────────────────

// runStreamLoop opens the CommandStream and retries on transient failures.
// An Unauthenticated status from the gateway means the control plane restarted
// and lost its in-memory registry; in that case the worker re-registers before
// reconnecting.
func runStreamLoop(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, client workerpb.WorkerGatewayClient, workerName string) error {
	backoff := retryBase

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.InfofCtx(ctx, "Opening CommandStream for worker %q...\n", workerName)

		err := runStream(ctx, rt, pr, client, workerName)
		if err == nil || ctx.Err() != nil {
			// Clean exit or context cancelled — stop retrying.
			return err
		}

		// Unauthenticated means the control plane lost its in-memory registry
		// (e.g. it restarted). The bootstrap token was already consumed during
		// Register so retrying would fail. Stop and tell the operator what to do.
		if isUnauthenticated(err) {
			return fmt.Errorf("worker join: gateway rejected the stream — "+
				"the control plane may have restarted; re-run 'catalog worker register' "+
				"and 'worker join' to reconnect: %w", err)
		}

		logger.WarningfCtx(ctx, "CommandStream disconnected (%v) — retrying in %s...\n", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*retryBackoffFactor, retryMax)
	}
}

// runStream opens one CommandStream, sends heartbeats, and drains incoming
// Commands until the stream is closed or an error occurs.
func runStream(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, client workerpb.WorkerGatewayClient, workerName string) error {
	stream, err := client.CommandStream(ctx)
	if err != nil {
		return fmt.Errorf("open CommandStream: %w", err)
	}

	// Send the first message so the gateway can identify which worker this is.
	if err := sendHeartbeat(stream, workerName); err != nil {
		return fmt.Errorf("initial heartbeat: %w", err)
	}

	logger.InfofCtx(ctx, "CommandStream open for worker %q \n", workerName)

	// Two concurrent activities:
	//   • recv goroutine: read Commands from the gateway and handle them.
	//   • heartbeat ticker: periodically send keep-alives.
	recvErrCh := make(chan error, 1)

	go func() {
		recvErrCh <- recvLoop(ctx, rt, pr, stream, workerName)
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-recvErrCh:
			return err

		case <-ticker.C:
			if err := sendHeartbeat(stream, workerName); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
	}
}

// recvLoop reads Commands from the gateway stream, dispatches each one to the
// local runtime, and sends the result back on the stream.
// The loop exits when the stream is closed or returns an error.
func recvLoop(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, stream grpc.BidiStreamingClient[workerpb.CommandResult, workerpb.Command], workerName string) error {
	for {
		cmd, err := stream.Recv()
		if err != nil {
			return err
		}

		logger.InfofCtx(ctx, "Worker %q received command id=%s type=%s\n",
			workerName, cmd.GetCommandId(), cmd.GetType())

		result := dispatch.Dispatch(ctx, rt, pr, cmd)
		result.WorkerName = workerName

		if err := stream.Send(result); err != nil {
			return fmt.Errorf("send command result id=%s: %w", cmd.GetCommandId(), err)
		}
	}
}

// sendHeartbeat sends a heartbeat CommandResult on the stream.
func sendHeartbeat(stream grpc.BidiStreamingClient[workerpb.CommandResult, workerpb.Command], workerName string) error {
	return stream.Send(&workerpb.CommandResult{
		WorkerName:  workerName,
		IsHeartbeat: true,
	})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// isUnauthenticated reports whether err carries gRPC status Unauthenticated.
func isUnauthenticated(err error) bool {
	return status.Code(err) == codes.Unauthenticated
}

// Made with Bob
