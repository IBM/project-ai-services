package stream

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

type mockRegistry struct {
	cmdCh      chan *workerpb.Command
	resultCh   chan *workerpb.CommandResult
	returnErr  error
	channelNil bool
}

func (m *mockRegistry) WaitForResult(workerName, commandID string) (chan *workerpb.CommandResult, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.resultCh, nil
}

func (m *mockRegistry) CancelWait(workerName, commandID string) {}

func (m *mockRegistry) WorkerCommandChannel(workerName string) (chan *workerpb.Command, bool) {
	if m.channelNil {
		return nil, false
	}
	return m.cmdCh, true
}

func (m *mockRegistry) WorkerRuntimeType(workerName string) (string, bool) {
	return "podman", true
}

func (m *mockRegistry) WorkerMetadata(workerName string) (map[string]string, bool) {
	return nil, true
}

func (m *mockRegistry) WorkerID(workerName string) (uuid.UUID, bool) {
	return uuid.Nil, true
}

func (m *mockRegistry) WorkerNameByID(id uuid.UUID) (string, bool) {
	return "", false
}

func (m *mockRegistry) WorkerInfoByID(ctx context.Context, id uuid.UUID) (name, runtimeType string) {
	return "", ""
}

func (m *mockRegistry) IsWorkerConnected(ctx context.Context, workerName string) bool {
	return true
}

func TestSendCancelFullChannelWarning(t *testing.T) {
	// Inject a short timeout via the struct field so the test does not block
	// for 5 s and avoids mutating shared package-level state.
	const testTimeout = 50 * time.Millisecond

	// Full channel capacity 1 with existing item
	cmdCh := make(chan *workerpb.Command, 1)
	cmdCh <- &workerpb.Command{CommandId: "blocker"}

	reg := &mockRegistry{cmdCh: cmdCh}
	s := New("test-worker", reg)
	s.cancelEnqueueTimeout = testTimeout

	start := time.Now()
	// sendCancel should wait up to cancelEnqueueTimeout and not panic or deadlock
	s.sendCancel("target-cmd-123")
	elapsed := time.Since(start)

	if elapsed < testTimeout {
		t.Fatalf("expected sendCancel to wait for timeout, elapsed: %v", elapsed)
	}
}
