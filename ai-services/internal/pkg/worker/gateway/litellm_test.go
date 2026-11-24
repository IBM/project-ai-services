package gateway

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestLiteLLMEgressRoute(t *testing.T) {
	t.Run("targets the control-plane private ingress over mTLS", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "example.com")
		t.Setenv(proxy.CaddyPrivateIngressPortEnvVar, "9444")

		r, err := liteLLMEgressRoute()

		require.NoError(t, err)
		assert.Equal(t, workerconstants.LiteLLMEgressRouteID, r.ID)
		assert.Equal(t, workerconstants.LiteLLMPathPrefix, r.PathPrefix)
		assert.Equal(t, "catalog-worker-gateway.example.com:9444", r.DialUpstream)
		assert.Equal(t, "catalog-worker-gateway.example.com", r.ServerName)
		assert.Equal(t, "/data/worker-tls/tls.crt", r.ClientCertPath)
		assert.Equal(t, "/data/worker-tls/tls.key", r.EncryptedClientKeyPath)
		assert.Equal(t, "/data/worker-tls/ca.crt", r.TrustedCACertPath)
		assert.Equal(t, "/etc/secret/worker-mtls-encryption-secret/mtls-encryption-key", r.SecretFilePath)
	})

	t.Run("defaults to the standard private ingress port", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "example.com")
		t.Setenv(proxy.CaddyPrivateIngressPortEnvVar, "")

		r, err := liteLLMEgressRoute()

		require.NoError(t, err)
		assert.Equal(t, "catalog-worker-gateway.example.com:"+proxy.DefaultPrivateIngressPort, r.DialUpstream)
	})

	t.Run("fails without a domain suffix", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "")

		_, err := liteLLMEgressRoute()

		assert.Error(t, err)
	})
}

func TestLiteLLMEgressEnabled(t *testing.T) {
	reg := registry.New(nil)
	g := &Gateway{registry: reg, runtimeType: types.RuntimeTypePodman}

	t.Run("not for the local worker", func(t *testing.T) {
		assert.False(t, g.liteLLMEgressEnabled(workerconstants.LocalWorkerName))
	})

	t.Run("not on an OpenShift control plane", func(t *testing.T) {
		og := &Gateway{registry: reg, runtimeType: types.RuntimeTypeOpenShift}
		assert.False(t, og.liteLLMEgressEnabled("w1"))
	})

	t.Run("not for a worker that is not connected", func(t *testing.T) {
		assert.False(t, g.liteLLMEgressEnabled("ghost"))
	})
}

func TestRegisterLiteLLMEgress_NoopWhenDisabled(t *testing.T) {
	rp := &recordingProxy{}
	orig := proxyFor
	proxyFor = func(*Gateway, string) proxy.ProxyManager { return rp }
	t.Cleanup(func() { proxyFor = orig })

	g := &Gateway{registry: registry.New(nil), runtimeType: types.RuntimeTypePodman}
	g.registerLiteLLMEgress(context.Background(), workerconstants.LocalWorkerName)

	assert.Empty(t, rp.egress)
}
