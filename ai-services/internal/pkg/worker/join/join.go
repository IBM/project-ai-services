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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/dispatch"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
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

// StartGrpcStream dials the catalog gRPC worker-gateway, registers with the
// bootstrap token, and holds the CommandStream open.
//
// Local-worker path: when opts.RequestedWorkerName == LocalWorkerName the
// caller has identified this as the co-located local worker. An empty token
// is permitted; the gateway applies the local-bypass registration path.
func StartGrpcStream(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, opts workertypes.GrpcStreamOptions) error {
	tlsDir := workerconstants.WorkerTLSDir
	// ── Step 1: Check for existing valid mTLS credentials & stream loop ────────────────────
	if hasValidTLSCredentials(ctx, tlsDir) {
		logger.InfofCtx(ctx, "worker join: valid mTLS credentials found in %s, skipping registration", tlsDir)

		workerName, err := workerNameFromCert(tlsDir)
		if err != nil {
			return fmt.Errorf("worker join: recover worker name from cert: %w", err)
		}

		return connectAndStream(ctx, rt, pr, opts.GatewayAddr, workerName)
	}

	// Require a token for all workers except the explicitly identified local worker.
	if opts.Token == "" && opts.RequestedWorkerName != workerconstants.LocalWorkerName {
		return fmt.Errorf("worker join: no valid mTLS credentials found in %s and no --token provided", tlsDir)
	}

	// ── Step 2: Register + stream loop ───────────────────────────────────────
	return runRegistrationLoop(ctx, rt, pr, opts)
}

// ─── registration loop ────────────────────────────────────────────────────────

// runRegistrationLoop calls Register and then enters the CommandStream retry
// loop.  If the stream comes back with codes.Unauthenticated it re-registers
// before reconnecting.
func runRegistrationLoop(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, opts workertypes.GrpcStreamOptions) error {
	workerName, err := register(ctx, opts, rt.Type())
	if err != nil {
		return fmt.Errorf("worker join: register: %w", err)
	}

	logger.InfofCtx(ctx, "Worker %q registered with control plane.\n", workerName)

	return connectAndStream(ctx, rt, pr, opts.GatewayAddr, workerName)
}

// register calls the Register RPC once and returns the worker name bound by
// the control plane.
func register(ctx context.Context, opts workertypes.GrpcStreamOptions, rt types.RuntimeType) (string, error) {
	logger.InfolnCtx(ctx, "Registering worker with catalog control plane...")

	tlsDir := workerconstants.WorkerTLSDir
	// 1. Generate local ECDSA P-256 key + CSR — private key never transmitted.
	keyPEM, csrPEM, err := generateKeyAndCSR()
	if err != nil {
		return "", err
	}

	// 2. Dial the gateway for bootstrap. ca.crt may not exist yet on first run,
	//    so buildTLSConfig falls back to InsecureSkipVerify (TOFU) if absent.
	tlsCfg, err := buildTLSConfig(tlsDir, nil)
	if err != nil {
		return "", err
	}
	if tlsCfg.InsecureSkipVerify {
		logger.WarningfCtx(ctx, "worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify")
	}

	conn, err := grpc.NewClient(opts.GatewayAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", opts.GatewayAddr, err)
	}
	defer func() { _ = conn.Close() }()

	// 3. Call Register with token + CSR.
	logger.InfolnCtx(ctx, "worker join: registering with catalog control plane...")
	resp, err := workerpb.NewWorkerGatewayClient(conn).Register(ctx, &workerpb.RegisterRequest{
		WorkerName:     opts.RequestedWorkerName,
		PreSharedToken: opts.Token,
		RuntimeType:    rt.String(),
		CsrPem:         csrPEM,
	})
	if err != nil {
		return "", fmt.Errorf("worker join: register RPC: %w", err)
	}

	// 4. Write TLS material to disk (see tls.go: writeTLSMaterial).
	if len(resp.GetTlsCertPem()) == 0 {
		return "", fmt.Errorf("gateway returned empty certificate — registration failed")
	}
	if err := writeTLSMaterial(tlsDir, resp.GetTlsCertPem(), keyPEM, resp.GetCaCertPem()); err != nil {
		return "", err
	}
	logger.InfofCtx(ctx, "worker join: mTLS credentials written to %s", tlsDir)

	// Recover the worker name from the signed cert — the gateway embeds the
	// token-bound worker name as the cert CN, so no separate response field is needed.
	return workerNameFromCert(tlsDir)
}

// ─── command-stream loop ──────────────────────────────────────────────────────

// connectAndStream loads mTLS credentials from tlsDir, dials the gateway with
// mTLS, and runs the CommandStream retry loop.
// workerName is sent in the first stream message so the gateway can identify
// this worker; it is empty on reconnect (the gateway will read it from the message).
func connectAndStream(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, gatewayAddr, workerName string) error {
	tlsDir := workerconstants.WorkerTLSDir
	cert, err := loadClientCert(tlsDir)
	if err != nil {
		return fmt.Errorf("worker join: %w", err)
	}

	tlsCfg, err := buildTLSConfig(tlsDir, &cert)
	if err != nil {
		return fmt.Errorf("worker join: build TLS config for stream: %w", err)
	}

	conn, err := grpc.NewClient(gatewayAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return fmt.Errorf("worker join: dial %s: %w", gatewayAddr, err)
	}
	defer func() { _ = conn.Close() }()

	logger.InfofCtx(ctx, "worker join: connecting as %q to %s", workerName, gatewayAddr)

	return runStreamLoop(ctx, rt, pr, workerpb.NewWorkerGatewayClient(conn), workerName)
}

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
