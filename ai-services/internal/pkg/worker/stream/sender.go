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

	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

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
		return nil, fmt.Errorf("stream: worker %s: command %s timed out after %s",
			s.workerName, cmdType, CommandTimeout)
	}
}
