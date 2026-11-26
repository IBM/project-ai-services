package routes

import (
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteUpstream(t *testing.T) {
	infos := []caddy.TemplateRouteInfo{
		{PodName: "ai-services--catalog", RoutesAnnotation: "8081:catalog-ui:ui, 8080:catalog-api:api"},
		{PodName: "ai-services--litellm", RoutesAnnotation: "4000:model-gateway:api"},
	}

	t.Run("returns the pod and port of the model-gateway route", func(t *testing.T) {
		got, err := routeUpstream(infos, "model-gateway")

		require.NoError(t, err)
		assert.Equal(t, "ai-services--litellm:4000", got)
	})

	t.Run("returns the pod and port of the catalog-api route", func(t *testing.T) {
		got, err := routeUpstream(infos, "catalog-api")

		require.NoError(t, err)
		assert.Equal(t, "ai-services--catalog:8080", got)
	})

	t.Run("fails when the route is missing", func(t *testing.T) {
		_, err := routeUpstream([]caddy.TemplateRouteInfo{
			{PodName: "ai-services--catalog", RoutesAnnotation: "8080:catalog-api:api"},
		}, "model-gateway")

		assert.Error(t, err)
	})

	t.Run("fails on a malformed annotation", func(t *testing.T) {
		_, err := routeUpstream([]caddy.TemplateRouteInfo{
			{PodName: "p", RoutesAnnotation: "bogus"},
		}, "model-gateway")

		assert.Error(t, err)
	})
}

// The catalog-auth ingress must expose exactly the three service-auth endpoints, nothing else.
func TestCatalogAuthRules(t *testing.T) {
	got := map[string]bool{}
	for _, r := range catalogAuthRules {
		got[r.Method+" "+r.Path] = true
	}

	assert.Equal(t, map[string]bool{
		"GET /.well-known/jwks.json":                 true,
		"POST /api/v1/auth/code/exchange":            true,
		"POST /api/v1/auth/service-credential/token": true,
	}, got)
}
