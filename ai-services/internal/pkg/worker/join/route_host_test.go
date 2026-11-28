package join

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// routeRuntime is a Runtime whose ListRoutes answers come from a script: each call returns the
// next entry, and the last one repeats.
type routeRuntime struct {
	runtime.Runtime
	rtType types.RuntimeType
	steps  []routeStep
	calls  int
}

type routeStep struct {
	routes []types.Route
	err    error
}

func (r *routeRuntime) Type() types.RuntimeType { return r.rtType }

func (r *routeRuntime) ListRoutes(context.Context, string) ([]types.Route, error) {
	step := r.steps[min(r.calls, len(r.steps)-1)]
	r.calls++

	return step.routes, step.err
}

func fastPoll(t *testing.T, timeout time.Duration) {
	t.Helper()

	origTimeout, origInterval := routeHostTimeout, routeHostPollInterval
	routeHostTimeout, routeHostPollInterval = timeout, time.Millisecond

	t.Cleanup(func() { routeHostTimeout, routeHostPollInterval = origTimeout, origInterval })
}

func TestWaitForRouteHost(t *testing.T) {
	name := workerconstants.WorkerCaddyRouteName

	t.Run("returns the host the cluster assigned", func(t *testing.T) {
		fastPoll(t, time.Second)

		rt := &routeRuntime{steps: []routeStep{{routes: []types.Route{
			{Name: "other", HostPort: "other.apps.example.com"},
			{Name: name, HostPort: "worker-caddy-ns.apps.example.com"},
		}}}}

		got, err := waitForRouteHost(context.Background(), rt, name)

		require.NoError(t, err)
		assert.Equal(t, "worker-caddy-ns.apps.example.com", got)
	})

	t.Run("waits until the host is set", func(t *testing.T) {
		fastPoll(t, time.Second)

		rt := &routeRuntime{steps: []routeStep{
			{routes: nil},
			{routes: []types.Route{{Name: name}}},
			{routes: []types.Route{{Name: name, HostPort: "worker-caddy-ns.apps.example.com"}}},
		}}

		got, err := waitForRouteHost(context.Background(), rt, name)

		require.NoError(t, err)
		assert.Equal(t, "worker-caddy-ns.apps.example.com", got)
		assert.Equal(t, 3, rt.calls)
	})

	t.Run("keeps waiting through a list error", func(t *testing.T) {
		fastPoll(t, time.Second)

		rt := &routeRuntime{steps: []routeStep{
			{err: errors.New("api unavailable")},
			{routes: []types.Route{{Name: name, HostPort: "h.example.com"}}},
		}}

		got, err := waitForRouteHost(context.Background(), rt, name)

		require.NoError(t, err)
		assert.Equal(t, "h.example.com", got)
	})

	t.Run("fails clearly when the host never appears", func(t *testing.T) {
		fastPoll(t, 30*time.Millisecond)

		rt := &routeRuntime{steps: []routeStep{{routes: []types.Route{{Name: name}}}}}

		_, err := waitForRouteHost(context.Background(), rt, name)

		require.Error(t, err)
		assert.ErrorContains(t, err, "has no host after")
		assert.ErrorContains(t, err, "no host assigned yet")
	})

	t.Run("fails when the route does not exist", func(t *testing.T) {
		fastPoll(t, 30*time.Millisecond)

		rt := &routeRuntime{steps: []routeStep{{routes: nil}}}

		_, err := waitForRouteHost(context.Background(), rt, name)

		require.ErrorContains(t, err, "not found")
	})

	t.Run("stops when the context is cancelled", func(t *testing.T) {
		fastPoll(t, time.Minute)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := waitForRouteHost(ctx, &routeRuntime{steps: []routeStep{{}}}, name)

		require.Error(t, err)
	})
}

func TestRegisterMetadata(t *testing.T) {
	t.Run("openshift reports the route host and the router port", func(t *testing.T) {
		meta := registerMetadata(context.Background(), types.RuntimeTypeOpenShift, "worker-caddy-ns.apps.example.com")

		assert.Equal(t, map[string]string{
			workerconstants.MetaKeyPrivateIngressHost: "worker-caddy-ns.apps.example.com",
			workerconstants.MetaKeyPrivateIngressPort: "443",
		}, meta)
	})

	t.Run("an openshift worker without Caddy reports neither", func(t *testing.T) {
		assert.Empty(t, registerMetadata(context.Background(), types.RuntimeTypeOpenShift, ""))
	})

	t.Run("podman reports its domain suffix and port, never a host", func(t *testing.T) {
		t.Setenv("DOMAIN_SUFFIX", "10.0.0.5.nip.io")
		t.Setenv("CADDY_PRIVATE_INGRESS_PORT", "9443")

		meta := registerMetadata(context.Background(), types.RuntimeTypePodman, "ignored.example.com")

		assert.Equal(t, map[string]string{
			workerconstants.MetaKeyDomainSuffix:       "10.0.0.5.nip.io",
			workerconstants.MetaKeyPrivateIngressPort: "9443",
		}, meta)
	})
}
