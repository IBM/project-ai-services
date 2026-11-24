package routes

import (
	"context"
	"errors"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
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

		r, err := LiteLLMEgressRoute()

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

		r, err := LiteLLMEgressRoute()

		require.NoError(t, err)
		assert.Equal(t, "catalog-worker-gateway.example.com:"+proxy.DefaultPrivateIngressPort, r.DialUpstream)
	})

	t.Run("fails without a domain suffix", func(t *testing.T) {
		t.Setenv(proxy.DomainSuffixEnvVar, "")

		_, err := LiteLLMEgressRoute()

		assert.Error(t, err)
	})
}

func TestRegisterLiteLLMEgress(t *testing.T) {
	t.Setenv(proxy.DomainSuffixEnvVar, "example.com")

	t.Run("success on first attempt", func(t *testing.T) {
		rp := &recordingProxy{}
		err := RegisterLiteLLMEgress(context.Background(), rp, "w1")
		require.NoError(t, err)
		require.Len(t, rp.egress, 1)
	})

	t.Run("retries on error", func(t *testing.T) {
		rp := &recordingProxy{errs: []error{errors.New("temporarily down")}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately to avoid sleeping in test
		err := RegisterLiteLLMEgress(ctx, rp, "w1")
		require.Error(t, err)
	})
}
