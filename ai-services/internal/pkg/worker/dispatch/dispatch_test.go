package dispatch

import (
	"context"
	"encoding/json"
	"testing"

	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
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

func TestHandleProxyRoute_PrivateRoutes(t *testing.T) {
	pr := workercaddy.NewInMemoryProxyRouter()

	send := func(p payload.ProxyRoute) error {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		_, err = handle(context.Background(), nil, pr, &workerpb.Command{
			Type:    workerpb.CommandType_COMMAND_TYPE_PROXY_ROUTE,
			Payload: b,
		})

		return err
	}

	// Every field the control plane sends must survive the JSON round trip and
	// reach the router; the in-memory router records the upstream.
	if err := send(payload.ProxyRoute{Op: payload.ProxyRouteOpRegisterIngress, ID: "i", PathPrefix: "/p", Upstream: "pod:8080"}); err != nil {
		t.Fatalf("register_ingress: %v", err)
	}
	if err := send(payload.ProxyRoute{
		Op: payload.ProxyRouteOpRegisterEgress, ID: "e", PathPrefix: "/p", Upstream: "10.0.0.2:9443",
		ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca", ServerName: "w1.example",
	}); err != nil {
		t.Fatalf("register_egress: %v", err)
	}

	for id, want := range map[string]string{"i": "pod:8080", "e": "10.0.0.2:9443"} {
		got, err := pr.ManageProxyRoute(context.Background(), payload.ProxyRouteOpGet, payload.Route{ID: id})
		if err != nil || got.Upstream != want {
			t.Errorf("route %s: got %+v, err %v; want upstream %q", id, got, err, want)
		}
	}
}
