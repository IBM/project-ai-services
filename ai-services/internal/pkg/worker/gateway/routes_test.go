package gateway

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"github.com/stretchr/testify/assert"
)

type recordingProxy struct {
	proxy.ProxyManager
	egress []proxy.EgressRoute
	errs   []error // returned in order, then nil
}

func (r *recordingProxy) RegisterEgressRoute(_ context.Context, route proxy.EgressRoute) error {
	r.egress = append(r.egress, route)
	if len(r.errs) == 0 {
		return nil
	}

	err := r.errs[0]
	r.errs = r.errs[1:]

	return err
}

func TestLiteLLMEgressEnabled(t *testing.T) {
	reg := registry.New(newFakeWorkerRepo())
	ctx := t.Context()

	register := func(name, runtime string, meta map[string]string) {
		t.Helper()

		_, err := reg.Register(ctx, name, runtime, meta)
		assert.NoError(t, err)
	}

	register("pw", "podman", map[string]string{workerconstants.MetaKeyDomainSuffix: "10.0.0.5.nip.io"})
	register("ow", "openshift", map[string]string{workerconstants.MetaKeyPrivateIngressHost: "caddy-ns.apps.example.com"})
	register("ow-nocaddy", "openshift", map[string]string{})

	for _, cpRuntime := range []types.RuntimeType{types.RuntimeTypePodman, types.RuntimeTypeOpenShift} {
		g := &Gateway{registry: reg, runtimeType: cpRuntime}

		t.Run(string(cpRuntime)+" control plane", func(t *testing.T) {
			assert.True(t, g.liteLLMEgressEnabled("pw"), "a Podman worker")
			assert.True(t, g.liteLLMEgressEnabled("ow"), "an OpenShift worker with a Caddy Route")
			assert.False(t, g.liteLLMEgressEnabled("ow-nocaddy"), "an OpenShift worker without a Caddy Route")
			assert.False(t, g.liteLLMEgressEnabled(workerconstants.LocalWorkerName), "the local worker")
			assert.False(t, g.liteLLMEgressEnabled("ghost"), "a worker that is not connected")
		})
	}
}

func TestLiteLLMIngressAddr(t *testing.T) {
	t.Run("podman control plane dials the gateway name on the private ingress port", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "example.com")
		t.Setenv(proxy.CaddyPrivateIngressPortEnvVar, "9444")

		host, port, err := liteLLMIngressAddr(t.Context(), types.RuntimeTypePodman)

		assert.NoError(t, err)
		assert.Equal(t, "catalog-worker-gateway.example.com", host)
		assert.Equal(t, "9444", port)
	})

	t.Run("podman control plane defaults the port", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "example.com")
		t.Setenv(proxy.CaddyPrivateIngressPortEnvVar, "")

		_, port, err := liteLLMIngressAddr(t.Context(), types.RuntimeTypePodman)

		assert.NoError(t, err)
		assert.Equal(t, proxy.DefaultPrivateIngressPort, port)
	})

	t.Run("podman control plane needs a domain suffix", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "")

		_, _, err := liteLLMIngressAddr(t.Context(), types.RuntimeTypePodman)

		assert.Error(t, err)
	})

	t.Run("unknown control plane runtime", func(t *testing.T) {
		_, _, err := liteLLMIngressAddr(t.Context(), "unknown")

		assert.Error(t, err)
	})
}

func TestRegisterLiteLLMEgress_UsesControlPlaneAddress(t *testing.T) {
	rp := &recordingProxy{}
	origProxy, origAddr := proxyFor, liteLLMIngressAddr
	proxyFor = func(*Gateway, string) proxy.ProxyManager { return rp }
	liteLLMIngressAddr = func(context.Context, types.RuntimeType) (string, string, error) {
		return "ai-services--caddy-ns.apps.example.com", "443", nil
	}
	t.Cleanup(func() { proxyFor, liteLLMIngressAddr = origProxy, origAddr })

	reg := registry.New(newFakeWorkerRepo())
	_, err := reg.Register(t.Context(), "pw", "podman", map[string]string{workerconstants.MetaKeyDomainSuffix: "10.0.0.5.nip.io"})
	assert.NoError(t, err)

	errCh := make(chan error, 1)
	g := &Gateway{registry: reg, runtimeType: types.RuntimeTypeOpenShift}
	g.registerLiteLLMEgress(t.Context(), "pw", errCh)

	assert.Empty(t, errCh)
	if assert.Len(t, rp.egress, 1) {
		assert.Equal(t, "ai-services--caddy-ns.apps.example.com:443", rp.egress[0].DialUpstream)
		assert.Equal(t, "ai-services--caddy-ns.apps.example.com", rp.egress[0].ServerName)
	}
}

func TestRegisterLiteLLMEgress_NoopWhenDisabled(t *testing.T) {
	rp := &recordingProxy{}
	orig := proxyFor
	proxyFor = func(*Gateway, string) proxy.ProxyManager { return rp }
	t.Cleanup(func() { proxyFor = orig })

	errCh := make(chan error, 1)
	g := &Gateway{registry: registry.New(nil), runtimeType: types.RuntimeTypePodman}
	g.registerLiteLLMEgress(context.Background(), workerconstants.LocalWorkerName, errCh)

	assert.Empty(t, rp.egress)
	assert.Empty(t, errCh)
}

func TestRegisterLiteLLMEgress_ReportsAddressError(t *testing.T) {
	orig := liteLLMIngressAddr
	liteLLMIngressAddr = func(context.Context, types.RuntimeType) (string, string, error) {
		return "", "", assert.AnError
	}
	t.Cleanup(func() { liteLLMIngressAddr = orig })

	reg := registry.New(newFakeWorkerRepo())
	_, err := reg.Register(t.Context(), "pw", "podman", map[string]string{workerconstants.MetaKeyDomainSuffix: "10.0.0.5.nip.io"})
	assert.NoError(t, err)

	errCh := make(chan error, 1)
	g := &Gateway{registry: reg, runtimeType: types.RuntimeTypeOpenShift}
	g.registerLiteLLMEgress(t.Context(), "pw", errCh)

	assert.Len(t, errCh, 1)
}
