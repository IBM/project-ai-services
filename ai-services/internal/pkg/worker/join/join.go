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
	"net"
	"sync"
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
func StartGrpcStream(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, opts workertypes.GrpcStreamOptions) error {
	if opts.GatewayAddr == "" {
		return fmt.Errorf("worker join: gateway address is required (e.g. gateway.10.0.0.1.nip.io:9090)")
	}
	if _, _, err := net.SplitHostPort(opts.GatewayAddr); err != nil {
		return fmt.Errorf("worker join: invalid gateway address %q — must be host:port (e.g. gateway.10.0.0.1.nip.io:9090)", opts.GatewayAddr)
	}

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

	if opts.Token == "" {
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
	tlsCfg, err := buildTLSConfig(opts.GatewayAddr, tlsDir, nil)
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

	tlsCfg, err := buildTLSConfig(gatewayAddr, tlsDir, &cert)
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
	// This is the only direct stream.Send call — after this, all writes go
	// through recvLoop's single sender goroutine to avoid concurrent sends.
	if err := sendHeartbeat(stream, workerName); err != nil {
		return fmt.Errorf("initial heartbeat: %w", err)
	}

	logger.InfofCtx(ctx, "CommandStream open for worker %q \n", workerName)

	// recvLoop owns all subsequent stream.Send calls (results + heartbeats).
	return recvLoop(ctx, rt, pr, stream, workerName)
}

// recvLoop reads Commands from the gateway stream and dispatches each one in
// its own goroutine so that long-running commands (e.g. HELM_INSTALL) do not
// block reception of subsequent commands.  Results and heartbeats are
// funnelled through a single send channel so that stream.Send is always
// called from one goroutine (gRPC streams are not safe for concurrent sends).
// The loop exits when the stream is closed or returns an error.
//
//nolint:cyclop // complexity comes from select branches, not logic depth
func recvLoop(ctx context.Context, rt runtime.Runtime, pr *workercaddy.ProxyRouter, stream grpc.BidiStreamingClient[workerpb.CommandResult, workerpb.Command], workerName string) error {
	// sendCh serialises all stream.Send calls — both command results and
	// heartbeats. Buffer of 32 prevents dispatch goroutines from blocking
	// on a momentarily busy sender.
	const sendBufSize = 32
	sendCh := make(chan *workerpb.CommandResult, sendBufSize)
	senderErrCh, senderDone := startSender(stream, sendCh)

	var wg sync.WaitGroup
	drain := makeDrainer(&wg, sendCh, senderDone, senderErrCh)

	// Heartbeat ticker — keep-alives go through sendCh so they share the
	// same stream.Send goroutine as command results.
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	heartbeat := &workerpb.CommandResult{WorkerName: workerName, IsHeartbeat: true}

	// recvCh carries commands (and the terminal error) from a background
	// stream.Recv goroutine so we can select on it with the ticker and ctx.
	recvCh := startRecvGoroutine(stream)

	// One Dispatcher per stream lifetime — it tracks all in-flight command
	// contexts so COMMAND_TYPE_CANCEL can abort a specific command.
	d := dispatch.New()

	for {
		select {
		case <-ctx.Done():
			return drain(ctx.Err())

		case <-ticker.C:
			select {
			case sendCh <- heartbeat:
			default: // drop heartbeat if sender is backed up; not critical
			}

		case msg := <-recvCh:
			if msg.err != nil {
				return drain(msg.err)
			}

			logger.InfofCtx(ctx, "Worker %q received command id=%s type=%s\n",
				workerName, msg.cmd.GetCommandId(), msg.cmd.GetType())

			wg.Add(1)

			go func(c *workerpb.Command) {
				defer wg.Done()

				result := d.Dispatch(ctx, rt, pr, c)
				result.WorkerName = workerName

				select {
				case sendCh <- result:
				case <-ctx.Done():
				}
			}(msg.cmd)
		}
	}
}

// makeDrainer returns a function that waits for all in-flight dispatch
// goroutines to finish, closes sendCh, waits for the sender goroutine to
// exit, and returns any sender error in preference to the recv error.
func makeDrainer(wg *sync.WaitGroup, sendCh chan *workerpb.CommandResult, senderDone <-chan struct{}, senderErrCh <-chan error) func(error) error {
	return func(recvErr error) error {
		wg.Wait()
		close(sendCh)
		<-senderDone

		select {
		case sErr := <-senderErrCh:
			return sErr
		default:
			return recvErr
		}
	}
}

// startSender starts the dedicated stream.Send goroutine and returns its error
// channel and done channel.
func startSender(stream grpc.BidiStreamingClient[workerpb.CommandResult, workerpb.Command], sendCh <-chan *workerpb.CommandResult) (chan error, chan struct{}) {
	senderErrCh := make(chan error, 1)
	senderDone := make(chan struct{})

	go func() {
		defer close(senderDone)

		for result := range sendCh {
			if err := stream.Send(result); err != nil {
				senderErrCh <- fmt.Errorf("send result/heartbeat id=%s: %w", result.GetCommandId(), err)

				return
			}
		}
	}()

	return senderErrCh, senderDone
}

// streamRecvMsg is a command received from the stream, or an error.
type streamRecvMsg struct {
	cmd *workerpb.Command
	err error
}

// startRecvGoroutine starts a goroutine that reads from stream.Recv and
// forwards each message (or error) to the returned channel.
func startRecvGoroutine(stream grpc.BidiStreamingClient[workerpb.CommandResult, workerpb.Command]) chan streamRecvMsg {
	recvCh := make(chan streamRecvMsg, 1)

	go func() {
		for {
			cmd, err := stream.Recv()
			recvCh <- streamRecvMsg{cmd, err}

			if err != nil {
				return
			}
		}
	}()

	return recvCh
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
