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
	mu       sync.Mutex
	known    map[string]bool
	posted   map[string][]map[string]any // server name -> routes
	status   int                         // status returned for route POST
	idStatus int                         // if set, status returned for GET /id/<unknown id> instead of 404
	bodies   map[string]any              // id -> JSON body returned by GET /id/<id> (optional)
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
			if body, ok := f.bodies[r.URL.Path[4:]]; ok {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)

				return
			}
			w.WriteHeader(http.StatusOK)

			return
		}
		if f.idStatus != 0 {
			w.WriteHeader(f.idStatus)

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
	assert.Equal(t, []any{"/models/m1", "/models/m1/*"}, match["path"], "trailing slash is normalised; bare prefix and subpaths both match")

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
	assert.Equal(t, []any{"/models/m1", "/models/m1/*"}, match["path"], "bare prefix and subpaths both match")

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

// A non-404 answer to the existence check means "unknown", not "absent": the
// route must not be created.
func TestCreateRouteIfAbsent_UnexpectedExistenceStatus(t *testing.T) {
	register := map[string]func(*caddyManager) error{
		"ingress": func(c *caddyManager) error {
			return c.RegisterIngressRoute(context.Background(), IngressRoute{ID: "i", PathPrefix: "/p", Upstream: "u:1"})
		},
		"egress": func(c *caddyManager) error {
			return c.RegisterEgressRoute(context.Background(), EgressRoute{
				ID: "e", PathPrefix: "/p", DialUpstream: "h:9443",
				ClientCertPath: "c", ClientKeyPath: "k", TrustedCACertPath: "ca",
			})
		},
		"public": func(c *caddyManager) error {
			_, err := c.RegisterRoute(context.Background(), Route{ID: "p", Domain: "app", Upstream: "u:1"})

			return err
		},
	}

	statuses := []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusUnauthorized, http.StatusForbidden}

	for _, status := range statuses {
		for kind, reg := range register {
			t.Run(http.StatusText(status)+"/"+kind, func(t *testing.T) {
				t.Setenv(DomainSuffixEnvVar, "example.com")
				f, c := newFakeCaddyAdmin(t)
				f.idStatus = status

				err := reg(c)

				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to check route existence")
				assert.Empty(t, f.posted, "no route may be created when existence is unknown")
			})
		}
	}
}

// RegisterRoute (public path) shares createRouteIfAbsent: it creates once on the
// public server, is idempotent, and returns the external URL.
func TestRegisterRoute_PublicIdempotent(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	t.Setenv(DomainSuffixEnvVar, "example.com")
	t.Setenv(CaddyHTTPSPortEnvVar, "8443")

	for range 2 {
		got, err := c.RegisterRoute(context.Background(), Route{ID: "app", Domain: "app", Upstream: "u:1", Terminal: true})
		require.NoError(t, err)
		assert.Equal(t, "https://app.example.com:8443", got)
	}

	require.Len(t, f.posted["ai_services"], 1)
	match := f.posted["ai_services"][0]["match"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"app.example.com"}, match["host"])
}

// Path-matched (private) routes have no host. GetRouteByID must return them
// rather than fail in extractDomainFromRoute.
func TestGetRouteByID_PathOnlyRoute(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	f.known["i"] = true
	f.bodies = map[string]any{"i": map[string]any{"@id": "i", "match": []any{map[string]any{"path": []any{"/p", "/p/*"}}}}}

	got, err := c.GetRouteByID(context.Background(), "i")
	require.NoError(t, err)
	assert.Equal(t, &Route{ID: "i"}, got)
}

// A route with neither host nor path is malformed and must still be an error.
func TestGetRouteByID_MalformedRouteStillErrors(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	f.known["bad"] = true
	f.bodies = map[string]any{"bad": map[string]any{"@id": "bad", "match": []any{map[string]any{"method": []any{"GET"}}}}}

	_, err := c.GetRouteByID(context.Background(), "bad")
	require.Error(t, err)
}

func TestGetRouteByID_HostRoute(t *testing.T) {
	f, c := newFakeCaddyAdmin(t)
	t.Setenv(CaddyHTTPSPortEnvVar, "443")
	f.known["h"] = true
	f.bodies = map[string]any{"h": map[string]any{"@id": "h", "match": []any{map[string]any{"host": []any{"app.example.com"}}}}}

	got, err := c.GetRouteByID(context.Background(), "h")
	require.NoError(t, err)
	assert.Equal(t, "app.example.com", got.Domain)
	assert.Equal(t, "https://app.example.com", got.ExternalURL)
}
