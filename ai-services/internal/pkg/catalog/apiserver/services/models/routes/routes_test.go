package routes

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProxy records the routes it is given. Only the methods the Manager uses are implemented.
type fakeProxy struct {
	proxy.ProxyManager
	ingress       []proxy.IngressRoute
	egress        []proxy.EgressRoute
	unregistered  []string
	ingressErr    error
	egressErr     error
	unregisterErr error
}

func (f *fakeProxy) RegisterIngressRoute(_ context.Context, r proxy.IngressRoute) error {
	f.ingress = append(f.ingress, r)

	return f.ingressErr
}

func (f *fakeProxy) RegisterEgressRoute(_ context.Context, r proxy.EgressRoute) error {
	f.egress = append(f.egress, r)

	return f.egressErr
}

func (f *fakeProxy) UnregisterRoute(_ context.Context, id string) error {
	f.unregistered = append(f.unregistered, id)

	return f.unregisterErr
}

type fakeDomains struct {
	suffix string
	port   string
	err    error
}

func (f fakeDomains) DomainSuffix(string) (string, error) { return f.suffix, f.err }
func (f fakeDomains) PrivateIngressPort(string) string {
	if f.port != "" {
		return f.port
	}
	return proxy.DefaultPrivateIngressPort
}

func newTestManager(domains DomainResolver, cp, worker *fakeProxy) *Manager {
	return &Manager{
		workers:      domains,
		cpProxy:      func() (proxy.ProxyManager, error) { return cp, nil },
		workerProxy:  func(runtime.Runtime) (proxy.ProxyManager, error) { return worker, nil },
		cpEgressHost: func() (string, error) { return "ai-services--caddy", nil },
	}
}

func TestRegister(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	cp, wk := &fakeProxy{}, &fakeProxy{}
	m := newTestManager(fakeDomains{suffix: "example.com"}, cp, wk)

	egress, err := m.Register(context.Background(), nil, "w1", id, "model-pod:8000")
	require.NoError(t, err)

	prefix := "/worker/w1/models/" + id.String()
	assert.Equal(t, "ai-services--caddy:8080"+prefix, egress, "LiteLLM reaches the model through the CP egress server")

	require.Len(t, wk.ingress, 1)
	assert.Equal(t, proxy.IngressRoute{ID: "model-" + id.String() + "--mtls", PathPrefix: prefix, Upstream: "model-pod:8000", Terminal: true}, wk.ingress[0],
		"the worker route strips the prefix and forwards to the pod")

	require.Len(t, cp.egress, 1)
	e := cp.egress[0]
	assert.Equal(t, "model-"+id.String()+"--egress", e.ID)
	assert.Equal(t, prefix, e.PathPrefix, "both routes share one prefix")
	assert.Equal(t, "w1.example.com:9443", e.DialUpstream, "the CP dials the name in the worker certificate")
	assert.Equal(t, "w1.example.com", e.ServerName, "and verifies that same name")
	assert.Equal(t, "/data/gateway-pki/server.crt", e.ClientCertPath)
	assert.Equal(t, "/data/gateway-pki/server.key", e.EncryptedClientKeyPath, "the encrypted key is used: no plaintext key path")
	assert.Equal(t, "/etc/secret/catalog-mtls-encryption-secret/mtls-encryption-key", e.SecretFilePath)
	assert.Equal(t, "/data/gateway-pki/ca.crt", e.TrustedCACertPath)
	assert.Empty(t, wk.unregistered, "nothing is rolled back on success")

	t.Run("custom worker private ingress port", func(t *testing.T) {
		customCP, customWK := &fakeProxy{}, &fakeProxy{}
		customM := newTestManager(fakeDomains{suffix: "example.com", port: "8443"}, customCP, customWK)

		_, err := customM.Register(context.Background(), nil, "w1", id, "model-pod:8000")
		require.NoError(t, err)
		require.Len(t, customCP.egress, 1)
		assert.Equal(t, "w1.example.com:8443", customCP.egress[0].DialUpstream)
	})
}

func TestRegister_Failures(t *testing.T) {
	id := uuid.New()

	t.Run("worker did not report a domain suffix", func(t *testing.T) {
		cp, wk := &fakeProxy{}, &fakeProxy{}
		m := newTestManager(fakeDomains{err: errors.New("no suffix")}, cp, wk)

		_, err := m.Register(context.Background(), nil, "w1", id, "pod:1")
		require.ErrorContains(t, err, "no suffix")
		assert.Empty(t, wk.ingress, "no route is created without a dial name")
		assert.Empty(t, cp.egress)
	})

	t.Run("ingress failure creates no egress route", func(t *testing.T) {
		cp, wk := &fakeProxy{}, &fakeProxy{ingressErr: errors.New("worker caddy down")}
		m := newTestManager(fakeDomains{suffix: "example.com"}, cp, wk)

		_, err := m.Register(context.Background(), nil, "w1", id, "pod:1")
		require.ErrorContains(t, err, "worker caddy down")
		assert.Empty(t, cp.egress)
	})

	t.Run("egress failure rolls the worker route back", func(t *testing.T) {
		cp, wk := &fakeProxy{egressErr: errors.New("cp caddy down")}, &fakeProxy{}
		m := newTestManager(fakeDomains{suffix: "example.com"}, cp, wk)

		_, err := m.Register(context.Background(), nil, "w1", id, "pod:1")
		require.ErrorContains(t, err, "cp caddy down")
		assert.Equal(t, []string{IngressRouteID(id)}, wk.unregistered, "no orphan worker route is left behind")
	})

	t.Run("control plane egress host unknown", func(t *testing.T) {
		cp, wk := &fakeProxy{}, &fakeProxy{}
		m := newTestManager(fakeDomains{suffix: "example.com"}, cp, wk)
		m.cpEgressHost = func() (string, error) { return "", errors.New("CADDY_ADMIN_URL not set") }

		_, err := m.Register(context.Background(), nil, "w1", id, "pod:1")
		require.ErrorContains(t, err, "CADDY_ADMIN_URL")
		assert.Empty(t, wk.ingress, "fail before touching either Caddy")
	})
}

func TestUnregister(t *testing.T) {
	id := uuid.New()

	t.Run("removes both routes", func(t *testing.T) {
		cp, wk := &fakeProxy{}, &fakeProxy{}
		require.NoError(t, newTestManager(nil, cp, wk).Unregister(context.Background(), nil, id))
		assert.Equal(t, []string{EgressRouteID(id)}, cp.unregistered)
		assert.Equal(t, []string{IngressRouteID(id)}, wk.unregistered)
	})

	t.Run("routes that are already gone are not an error", func(t *testing.T) {
		cp, wk := &fakeProxy{unregisterErr: proxy.ErrRouteNotFound}, &fakeProxy{unregisterErr: errors.New("stream: command failed: route not found")}
		require.NoError(t, newTestManager(nil, cp, wk).Unregister(context.Background(), nil, id),
			"delete must stay retryable: a missing route, local or relayed by a worker, is success")
	})

	t.Run("a failure on one side still removes the other", func(t *testing.T) {
		cp, wk := &fakeProxy{unregisterErr: errors.New("cp caddy down")}, &fakeProxy{}
		err := newTestManager(nil, cp, wk).Unregister(context.Background(), nil, id)
		require.ErrorContains(t, err, "cp caddy down")
		assert.Equal(t, []string{IngressRouteID(id)}, wk.unregistered)
	})
}

func TestCaddyHostFromAdminURL(t *testing.T) {
	t.Setenv(proxy.CaddyAdminURLEnvVar, "http://ai-services--caddy:2019")
	host, err := caddyHostFromAdminURL()
	require.NoError(t, err)
	assert.Equal(t, "ai-services--caddy", host)

	t.Setenv(proxy.CaddyAdminURLEnvVar, "")
	_, err = caddyHostFromAdminURL()
	require.Error(t, err)
}

func TestRemoteWorkerProxy_RejectsNonRemoteRuntime(t *testing.T) {
	_, err := remoteWorkerProxy(nil)
	require.Error(t, err)
}
