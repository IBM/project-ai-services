package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// routeLister is a Runtime that only answers ListRoutes; any other call panics on the nil embed.
type routeLister struct {
	runtime.Runtime
	routes   []types.Route
	err      error
	selector string
}

func (r *routeLister) ListRoutes(_ context.Context, selector string) ([]types.Route, error) {
	r.selector = selector

	return r.routes, r.err
}

func TestRouteHost(t *testing.T) {
	t.Run("finds the route by name, whatever its labels", func(t *testing.T) {
		rt := &routeLister{routes: []types.Route{
			{Name: "other", HostPort: "other.apps.example.com"},
			{Name: "worker-caddy", HostPort: "worker-caddy-ns.apps.example.com"},
		}}

		got, err := runtime.RouteHost(t.Context(), rt, "worker-caddy")
		require.NoError(t, err)
		assert.Equal(t, "worker-caddy-ns.apps.example.com", got)
		assert.Empty(t, rt.selector, "no label selector: the lookup is by name")
	})

	t.Run("a route without a host yet is an error", func(t *testing.T) {
		rt := &routeLister{routes: []types.Route{{Name: "worker-caddy"}}}

		_, err := runtime.RouteHost(t.Context(), rt, "worker-caddy")
		assert.ErrorContains(t, err, "no host assigned yet")
	})

	t.Run("a missing route is an error", func(t *testing.T) {
		_, err := runtime.RouteHost(t.Context(), &routeLister{}, "worker-caddy")
		assert.ErrorContains(t, err, "not found")
	})

	t.Run("a list failure is returned", func(t *testing.T) {
		_, err := runtime.RouteHost(t.Context(), &routeLister{err: errors.New("boom")}, "worker-caddy")
		assert.ErrorContains(t, err, "boom")
	})
}
