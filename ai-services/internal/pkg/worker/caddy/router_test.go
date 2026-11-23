package caddy

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
)

func TestManageProxyRoute_PrivateRoutes(t *testing.T) {
	pr := NewInMemoryProxyRouter()
	ctx := context.Background()

	if _, err := pr.ManageProxyRoute(ctx, payload.ProxyRouteOpRegisterIngress, payload.Route{
		ID: "m1-ingress", PathPrefix: "/models/m1", Upstream: "pod:8080",
	}); err != nil {
		t.Fatalf("register ingress: %v", err)
	}

	if _, err := pr.ManageProxyRoute(ctx, payload.ProxyRouteOpRegisterEgress, payload.Route{
		ID: "m1-egress", PathPrefix: "/models/m1", Upstream: "10.0.0.2:9443",
		ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca",
	}); err != nil {
		t.Fatalf("register egress: %v", err)
	}

	for id, upstream := range map[string]string{"m1-ingress": "pod:8080", "m1-egress": "10.0.0.2:9443"} {
		got, err := pr.ManageProxyRoute(ctx, payload.ProxyRouteOpGet, payload.Route{ID: id})
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if got.Upstream != upstream {
			t.Errorf("route %s: upstream = %q, want %q", id, got.Upstream, upstream)
		}
	}

	// Private routes are removed with the existing unregister op.
	if _, err := pr.ManageProxyRoute(ctx, payload.ProxyRouteOpUnregister, payload.Route{ID: "m1-ingress"}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if _, err := pr.ManageProxyRoute(ctx, payload.ProxyRouteOpGet, payload.Route{ID: "m1-ingress"}); err == nil {
		t.Error("expected route to be gone after unregister")
	}
}

func TestManageProxyRoute_UnsupportedOp(t *testing.T) {
	if _, err := NewInMemoryProxyRouter().ManageProxyRoute(context.Background(), "bogus", payload.Route{}); err == nil {
		t.Fatal("expected error for unsupported op")
	}
}
