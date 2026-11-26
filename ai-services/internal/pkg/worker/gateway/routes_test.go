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

	errCh := make(chan error, 1)
	g := &Gateway{registry: registry.New(nil), runtimeType: types.RuntimeTypePodman}
	g.registerLiteLLMEgress(context.Background(), workerconstants.LocalWorkerName, errCh)

	assert.Empty(t, rp.egress)
	assert.Empty(t, errCh)
}
