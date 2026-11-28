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

func TestControlPlaneEgressEnabled(t *testing.T) {
	reg := registry.New(nil)
	g := &Gateway{registry: reg, runtimeType: types.RuntimeTypePodman}

	t.Run("not for the local worker", func(t *testing.T) {
		assert.False(t, g.controlPlaneEgressEnabled(workerconstants.LocalWorkerName))
	})

	t.Run("not on an OpenShift control plane", func(t *testing.T) {
		og := &Gateway{registry: reg, runtimeType: types.RuntimeTypeOpenShift}
		assert.False(t, og.controlPlaneEgressEnabled("w1"))
	})

	t.Run("not for a worker that is not connected", func(t *testing.T) {
		assert.False(t, g.controlPlaneEgressEnabled("ghost"))
	})
}

func TestRegisterControlPlaneEgress_NoopWhenDisabled(t *testing.T) {
	rp := &recordingProxy{}
	orig := proxyFor
	proxyFor = func(*Gateway, string) proxy.ProxyManager { return rp }
	t.Cleanup(func() { proxyFor = orig })

	errCh := make(chan error, 1)
	g := &Gateway{registry: registry.New(nil), runtimeType: types.RuntimeTypePodman}
	g.registerControlPlaneEgress(context.Background(), workerconstants.LocalWorkerName, errCh)

	assert.Empty(t, rp.egress)
	assert.Empty(t, errCh)
}

func TestRegisterControlPlaneEgress_RegistersLiteLLMAndCatalogAuth(t *testing.T) {
	t.Setenv(proxy.DomainSuffixEnvVar, "example.com")

	rp := &recordingProxy{}
	orig := proxyFor
	proxyFor = func(*Gateway, string) proxy.ProxyManager { return rp }
	t.Cleanup(func() { proxyFor = orig })

	reg := registry.New(nil)
	_, err := reg.Register(context.Background(), "w1", types.RuntimeTypePodman.String(), nil)
	require.NoError(t, err)
	g := &Gateway{registry: reg, runtimeType: types.RuntimeTypePodman}

	errCh := make(chan error, 1)
	g.registerControlPlaneEgress(context.Background(), "w1", errCh)

	require.Len(t, rp.egress, 2)
	assert.Equal(t, workerconstants.LiteLLMEgressRouteID, rp.egress[0].ID)
	assert.Equal(t, workerconstants.CatalogAuthEgressRouteID, rp.egress[1].ID)
	assert.Empty(t, errCh)
}
