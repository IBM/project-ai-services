package caddy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiteLLMUpstream(t *testing.T) {
	t.Run("returns the pod and port of the model-gateway route", func(t *testing.T) {
		infos := []TemplateRouteInfo{
			{PodName: "ai-services--catalog", RoutesAnnotation: "8081:catalog-ui:ui, 8080:catalog-api:api"},
			{PodName: "ai-services--litellm", RoutesAnnotation: "4000:model-gateway:api"},
		}

		got, err := liteLLMUpstream(infos)

		require.NoError(t, err)
		assert.Equal(t, "ai-services--litellm:4000", got)
	})

	t.Run("fails when there is no model-gateway route", func(t *testing.T) {
		_, err := liteLLMUpstream([]TemplateRouteInfo{
			{PodName: "ai-services--catalog", RoutesAnnotation: "8080:catalog-api:api"},
		})

		assert.Error(t, err)
	})

	t.Run("fails on a malformed annotation", func(t *testing.T) {
		_, err := liteLLMUpstream([]TemplateRouteInfo{
			{PodName: "p", RoutesAnnotation: "bogus"},
		})

		assert.Error(t, err)
	})
}
