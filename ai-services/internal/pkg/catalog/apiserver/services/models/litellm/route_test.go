package litellm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestGatewayAddress(t *testing.T) {
	t.Run("podman, local worker: the gateway pod", func(t *testing.T) {
		assert.Equal(t,
			Address{Host: constants.LiteLLMPodName, Port: constants.LiteLLMPort},
			GatewayAddress(runtimeTypes.RuntimeTypePodman, false))
	})

	t.Run("podman, remote worker: the worker's Caddy private egress under /litellm", func(t *testing.T) {
		assert.Equal(t,
			Address{Host: "ai-services--caddy", Port: "8080", PathPrefix: "/litellm"},
			GatewayAddress(runtimeTypes.RuntimeTypePodman, true))
	})

	t.Run("openshift: the gateway service by its cluster-wide name", func(t *testing.T) {
		assert.Equal(t,
			Address{Host: "litellm.ai-services.svc.cluster.local", Port: constants.LiteLLMPort},
			GatewayAddress(runtimeTypes.RuntimeTypeOpenShift, false))
	})
}

func TestAddress_TokenizeEndpoint(t *testing.T) {
	id := uuid.New()

	assert.Equal(t, "http://litellm:4000/tokenize/"+id.String(),
		Address{Host: "litellm", Port: "4000"}.TokenizeEndpoint(id),
		"the passthrough is named after the component ID")
	assert.Equal(t, "http://ai-services--caddy:8080/litellm/tokenize/"+id.String(),
		GatewayAddress(runtimeTypes.RuntimeTypePodman, true).TokenizeEndpoint(id),
		"through a worker's egress the route prefix comes first")
}

func TestInternalKeyAlias(t *testing.T) {
	id := uuid.New()

	assert.Equal(t, "granite-"+id.String()+"-app-slug", InternalKeyAlias("granite", id, "slug"))
	assert.NotEqual(t, PublicKeyAlias("granite", id), InternalKeyAlias("granite", id, "slug"))
}
