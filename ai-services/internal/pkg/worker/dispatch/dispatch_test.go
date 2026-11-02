package dispatch

import (
	"context"
	"testing"
)

func TestCancelBeforeRegisterRace(t *testing.T) {
	d := New()
	cmdID := "cmd-123"

	// 1. Cancel arrives before register()
	d.cancelCommand(cmdID)

	// Verify tombstone is recorded
	d.mu.Lock()
	if _, ok := d.cancelled[cmdID]; !ok {
		t.Fatalf("expected tombstone in cancelled map for %s", cmdID)
	}
	d.mu.Unlock()

	// 2. Target command calls register()
	ctx := d.register(context.Background(), cmdID)

	// Context should already be cancelled
	if ctx.Err() == nil {
		t.Fatalf("expected context to be cancelled, got nil error")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", ctx.Err())
	}

	// Tombstone should be consumed
	d.mu.Lock()
	if _, ok := d.cancelled[cmdID]; ok {
		t.Fatalf("expected tombstone to be cleared after register()")
	}
	if _, ok := d.inflight[cmdID]; ok {
		t.Fatalf("expected command not to be in inflight map")
	}
	d.mu.Unlock()

	// Deregister clean up should work safely
	d.deregister(cmdID)
}

func TestNormalCancelFlow(t *testing.T) {
	d := New()
	cmdID := "cmd-456"

	// Register first
	ctx := d.register(context.Background(), cmdID)
	if ctx.Err() != nil {
		t.Fatalf("context should not be cancelled yet, got %v", ctx.Err())
	}

	// Cancel in-flight
	d.cancelCommand(cmdID)
	if ctx.Err() == nil {
		t.Fatalf("expected context to be cancelled after cancelCommand")
	}

	// Deregister
	d.deregister(cmdID)
}
