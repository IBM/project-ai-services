// Package stream provides the Sender primitive for sending Commands to a worker
// over the gRPC CommandStream and waiting for results. It is imported by both
// runtime/remote and proxy so neither duplicates the send/receive logic.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// cancelEnqueueTimeout is the maximum time sendCancel will wait to place the
// COMMAND_TYPE_CANCEL message onto the worker's command channel. The channel
// has a fixed capacity (32); under normal conditions a slot is available
// immediately. 5 s is enough to outlast any transient back-pressure without
// risking an indefinite block.
const cancelEnqueueTimeout = 5 * time.Second

const CommandTimeout = 10 * time.Minute

// Sender encapsulates the logic for sending a Command to a worker over the
// gRPC CommandStream and waiting for its CommandResult.
type Sender struct {
	workerName string
	registry   WorkerRegistry
}

// New returns a Sender targeting the named worker.
func New(workerName string, reg WorkerRegistry) *Sender {
	return &Sender{workerName: workerName, registry: reg}
}

// WorkerName returns the name of the worker this Sender targets.
func (s *Sender) WorkerName() string {
	return s.workerName
}

// Send encodes payload as JSON, enqueues the Command on the worker's channel,
// and blocks until the worker returns a CommandResult or ctx/timeout expires.
//
// If ctx is cancelled while waiting for the worker's result, Send sends a
// COMMAND_TYPE_CANCEL to the worker on a best-effort basis so it stops the
// in-flight work (e.g. aborts a Helm install or stops a model-download
// container). The cancel is fire-and-forget — Send returns ctx.Err()
// immediately regardless of whether the cancel message was delivered.
func (s *Sender) Send(ctx context.Context, cmdType workerpb.CommandType, payload any) (*workerpb.CommandResult, error) {
	commandID := uuid.New().String()

	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("stream: marshal payload for %s: %w", cmdType, err)
		}
	}

	// Register result channel BEFORE sending to avoid a race where the worker
	// responds before we start listening.
	resultCh, err := s.registry.WaitForResult(s.workerName, commandID)
	if err != nil {
		return nil, fmt.Errorf("stream: worker %s not connected: %w", s.workerName, err)
	}

	cmdCh, ok := s.registry.WorkerCommandChannel(s.workerName)
	if !ok {
		return nil, fmt.Errorf("stream: worker %s disconnected", s.workerName)
	}

	cmd := &workerpb.Command{
		CommandId: commandID,
		Type:      cmdType,
		Payload:   payloadBytes,
	}

	select {
	case cmdCh <- cmd:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return s.waitForResult(ctx, cmdType, commandID, resultCh)
}

// waitForResult blocks until the worker returns a result for commandID, the
// context is cancelled, or CommandTimeout elapses. On cancellation or timeout
// it fires a best-effort COMMAND_TYPE_CANCEL to the worker.
func (s *Sender) waitForResult(ctx context.Context, cmdType workerpb.CommandType, commandID string, resultCh <-chan *workerpb.CommandResult) (*workerpb.CommandResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	select {
	case res := <-resultCh:
		if !res.GetSuccess() {
			return nil, fmt.Errorf("stream: worker %s: command %s failed: %s",
				s.workerName, cmdType, res.GetError())
		}

		return res, nil

	case <-timeoutCtx.Done():
		// ctx was cancelled (mid-deployment delete) or the command timed out.
		// Send a best-effort COMMAND_TYPE_CANCEL so the worker stops the in-flight
		// work (aborts Helm install, stops model-download container, etc.).
		// Use a fresh background context — the caller's ctx is already done.
		s.sendCancel(commandID)

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, fmt.Errorf("stream: worker %s: command %s timed out after %s",
			s.workerName, cmdType, CommandTimeout)
	}
}

// sendCancel fires a COMMAND_TYPE_CANCEL for commandID to the worker.
// It blocks for up to cancelEnqueueTimeout waiting for a slot in the worker's
// command channel — this ensures the cancel is not silently dropped when the
// channel is momentarily full (e.g. during a large architecture deployment
// with many concurrent Helm installs). Errors are silently ignored — the
// caller (Send) already has the error it needs to return.
func (s *Sender) sendCancel(commandID string) {
	cancelPayload, err := json.Marshal(payload.CancelCommand{CommandID: commandID})
	if err != nil {
		return
	}

	cmdCh, ok := s.registry.WorkerCommandChannel(s.workerName)
	if !ok {
		return // worker disconnected — nothing to cancel
	}

	cancelCmd := &workerpb.Command{
		CommandId: uuid.New().String(),
		Type:      workerpb.CommandType_COMMAND_TYPE_CANCEL,
		Payload:   cancelPayload,
	}

	// Use a short timeout context so we never block indefinitely if the worker
	// is overwhelmed or disconnected between WorkerCommandChannel and here.
	ctx, cancel := context.WithTimeout(context.Background(), cancelEnqueueTimeout)
	defer cancel()

	select {
	case cmdCh <- cancelCmd:
	case <-ctx.Done():
		// Channel still full after cancelEnqueueTimeout — worker is either
		// overwhelmed or has disconnected. Best-effort: give up rather than block.
	}
}
