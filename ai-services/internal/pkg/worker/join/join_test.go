package join

import (
	"context"
	"fmt"
	"testing"
	"time"

	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"google.golang.org/grpc/metadata"
)

// fakeStream is a minimal BidiStreamingClient that lets tests inject send errors.
type fakeStream struct {
	recvCh    chan *workerpb.Command
	sendErrCh chan error // first receive from this triggers Send to fail
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		recvCh:    make(chan *workerpb.Command),
		sendErrCh: make(chan error, 1),
	}
}

func (f *fakeStream) Send(r *workerpb.CommandResult) error {
	select {
	case err := <-f.sendErrCh:
		return err
	default:
		return nil
	}
}

func (f *fakeStream) Recv() (*workerpb.Command, error) {
	cmd, ok := <-f.recvCh
	if !ok {
		return nil, fmt.Errorf("stream closed")
	}
	return cmd, nil
}

func (f *fakeStream) Header() (metadata.MD, error)  { return nil, nil }
func (f *fakeStream) Trailer() metadata.MD           { return nil }
func (f *fakeStream) CloseSend() error               { return nil }
func (f *fakeStream) Context() context.Context       { return context.Background() }
func (f *fakeStream) SendMsg(m any) error            { return nil }
func (f *fakeStream) RecvMsg(m any) error            { return nil }

// TestRecvLoop_SenderError_DoesNotDeadlock verifies that when stream.Send fails,
// recvLoop drains cleanly and returns — it must not deadlock in wg.Wait().
// Before the fix, dispatch goroutines blocked on sendCh <- result forever
// because the sender had exited but ctx was not cancelled.
func TestRecvLoop_SenderError_DoesNotDeadlock(t *testing.T) {
	stream := newFakeStream()

	// Inject a send error on the very next Send call.
	stream.sendErrCh <- fmt.Errorf("simulated network error")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		// recvLoop will: open sender goroutine → sender fails immediately on
		// first Send (the heartbeat or a result) → streamCtx cancelled →
		// recv goroutine exits when stream.recvCh is closed → drain returns.
		done <- recvLoop(ctx, nil, nil, stream, "test-worker")
	}()

	// Close the recv side so the recv goroutine unblocks and posts an error.
	close(stream.recvCh)

	select {
	case <-done:
		// recvLoop returned — no deadlock.
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock: recvLoop did not return within 3s after sender error")
	}
}
