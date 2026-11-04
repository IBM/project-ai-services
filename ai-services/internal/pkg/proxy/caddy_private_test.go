package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCaddyAdmin is a minimal Caddy admin API: it answers /config (health),
// /id/<id> (existence) and records POSTs to /config/apps/http/servers/<name>/routes.
type fakeCaddyAdmin struct {
	mu     sync.Mutex
	known  map[string]bool
	posted map[string][]map[string]any // server name -> routes
	status int                         // status returned for route POST
}

func newFakeCaddyAdmin(t *testing.T) (*fakeCaddyAdmin, *caddyManager) {
	t.Helper()

	f := &fakeCaddyAdmin{known: map[string]bool{}, posted: map[string][]map[string]any{}, status: http.StatusOK}

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	return f, NewCaddyManager(srv.URL, "ai_services").(*caddyManager)
}

func (f *fakeCaddyAdmin) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const routesSuffix = "/routes"

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/config":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && len(r.URL.Path) > len("/id/") && r.URL.Path[:4] == "/id/":
		if f.known[r.URL.Path[4:]] {
			w.WriteHeader(http.StatusOK)

			return
		}
		w.WriteHeader(http.StatusNotFound)
	case r.Method == http.MethodPost:
		const prefix = "/config/apps/http/servers/"
		path := r.URL.Path
		server := path[len(prefix) : len(path)-len(routesSuffix)]
		body, _ := io.ReadAll(r.Body)
		var route map[string]any
		_ = json.Unmarshal(body, &route)
		if f.status == http.StatusOK {
			f.posted[server] = append(f.posted[server], route)
			if id, ok := route["@id"].(string); ok {
				f.known[id] = true
			}
		}
		w.WriteHeader(f.status)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRegisterIngressRoute(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)

	err := c.RegisterIngressRoute(context.Background(), IngressRoute{
		ID: "m1-ingress", PathPrefix: "/models/m1/", Upstream: "pod:8080", Terminal: true,
	})
	require.NoError(t, err)

	routes := f.posted[CaddyIngressServerName]
	require.Len(t, routes, 1, "route must be posted to the private ingress server")
	assert.Empty(t, f.posted["ai_services"], "public server must not be touched")

	r := routes[0]
	assert.Equal(t, "m1-ingress", r["@id"])
	assert.Equal(t, true, r["terminal"])

	match := r["match"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"/models/m1/*"}, match["path"], "trailing slash is normalised")

	handle := r["handle"].([]any)
	require.Len(t, handle, 2)
	assert.Equal(t, "rewrite", handle[0].(map[string]any)["handler"])
	assert.Equal(t, "/models/m1", handle[0].(map[string]any)["strip_path_prefix"], "prefix is stripped before proxying")
	assert.Equal(t, "reverse_proxy", handle[1].(map[string]any)["handler"])
}

func TestRegisterEgressRoute(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)

	err := c.RegisterEgressRoute(context.Background(), EgressRoute{
		ID: "m1-egress", PathPrefix: "/models/m1", DialUpstream: "10.0.0.2:9443",
		ClientCertPath: "/data/gateway-pki/server.crt", ClientKeyPath: "/data/gateway-pki/server.key.pem",
		TrustedCACertPath: "/data/gateway-pki/ca.crt", ServerName: "w1.10.0.0.2.nip.io",
	})
	require.NoError(t, err)

	routes := f.posted[CaddyEgressServerName]
	require.Len(t, routes, 1, "route must be posted to the private egress server")

	r := routes[0]
	assert.Equal(t, "m1-egress", r["@id"])
	match := r["match"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"/models/m1/*"}, match["path"])

	handle := r["handle"].([]any)
	require.Len(t, handle, 1, "egress must not rewrite: the remote ingress strips the prefix")

	h := handle[0].(map[string]any)
	assert.Equal(t, "reverse_proxy", h["handler"])
	assert.Equal(t, []any{map[string]any{"dial": "10.0.0.2:9443"}}, h["upstreams"])

	tlsCfg := h["transport"].(map[string]any)["tls"].(map[string]any)
	assert.Equal(t, "/data/gateway-pki/server.crt", tlsCfg["client_certificate_file"])
	assert.Equal(t, "/data/gateway-pki/server.key.pem", tlsCfg["client_certificate_key_file"])
	assert.Equal(t, "w1.10.0.0.2.nip.io", tlsCfg["server_name"])
	assert.Equal(t, map[string]any{"provider": "file", "pem_files": []any{"/data/gateway-pki/ca.crt"}}, tlsCfg["ca"])
}

func TestRegisterEgressRoute_OmitsServerNameWhenUnset(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)

	require.NoError(t, c.RegisterEgressRoute(context.Background(), EgressRoute{
		ID: "e", PathPrefix: "/p", DialUpstream: "h:9443",
		ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca",
	}))

	h := f.posted[CaddyEgressServerName][0]["handle"].([]any)[0].(map[string]any)
	tlsCfg := h["transport"].(map[string]any)["tls"].(map[string]any)
	assert.NotContains(t, tlsCfg, "server_name")
}

func TestRegisterPrivateRoutes_Idempotent(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	ctx := context.Background()

	in := IngressRoute{ID: "i", PathPrefix: "/p", Upstream: "u:1"}
	eg := EgressRoute{ID: "e", PathPrefix: "/p", DialUpstream: "h:9443", ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca"}

	for range 2 {
		require.NoError(t, c.RegisterIngressRoute(ctx, in))
		require.NoError(t, c.RegisterEgressRoute(ctx, eg))
	}

	assert.Len(t, f.posted[CaddyIngressServerName], 1, "an existing @id must not be registered twice")
	assert.Len(t, f.posted[CaddyEgressServerName], 1)
}

func TestRegisterPrivateRoutes_Validation(t *testing.T) {
	_, c := newFakeCaddyAdmin(t)
	ctx := context.Background()

	okEgress := EgressRoute{ID: "e", PathPrefix: "/p", DialUpstream: "h:9443", ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca"}

	cases := map[string]error{
		"ingress without id":         c.RegisterIngressRoute(ctx, IngressRoute{PathPrefix: "/p", Upstream: "u"}),
		"ingress without prefix":     c.RegisterIngressRoute(ctx, IngressRoute{ID: "i", Upstream: "u"}),
		"ingress without upstream":   c.RegisterIngressRoute(ctx, IngressRoute{ID: "i", PathPrefix: "/p"}),
		"egress without id":          c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.ID = "" })),
		"egress without prefix":      c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.PathPrefix = "" })),
		"egress without upstream":    c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.DialUpstream = "" })),
		"egress without client cert": c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.ClientCertPath = "" })),
		"egress without client key":  c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.ClientKeyPath = "" })),
		"egress without trusted CA":  c.RegisterEgressRoute(ctx, withEgress(okEgress, func(r *EgressRoute) { r.TrustedCACertPath = "" })),
	}

	for name, err := range cases {
		assert.Error(t, err, name)
	}
}

func TestRegisterPrivateRoutes_CaddyRejects(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	f.status = http.StatusInternalServerError

	err := c.RegisterIngressRoute(context.Background(), IngressRoute{ID: "i", PathPrefix: "/p", Upstream: "u:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), CaddyIngressServerName, "error must name the server the route was posted to")
}

func withEgress(r EgressRoute, mutate func(*EgressRoute)) EgressRoute {
	mutate(&r)

	return r
}
