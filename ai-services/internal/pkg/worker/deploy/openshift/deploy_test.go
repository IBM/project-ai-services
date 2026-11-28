package openshift

import (
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTemplates records the CLI overrides prepareValues passes to LoadValues.
type fakeTemplates struct {
	templates.Template
	overrides map[string]string
}

func (f *fakeTemplates) LoadValues(_ string, _ []string, cliOverrides map[string]string) (map[string]interface{}, error) {
	f.overrides = cliOverrides

	return map[string]interface{}{}, nil
}

func TestPrepareValues(t *testing.T) {
	t.Run("a remote worker enables its Caddy", func(t *testing.T) {
		tp := &fakeTemplates{}

		_, err := prepareValues(tp, workertypes.OpenshiftWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				Token:       "tok",
				GatewayAddr: "catalog-worker-gateway-ns.apps.example.com:443",
			},
			CaddyEnabled: true,
		})

		require.NoError(t, err)
		assert.Equal(t, "true", tp.overrides[workerconstants.ArgParamCaddyEnabled])
		assert.Equal(t, "tok", tp.overrides[workerconstants.ArgParamWorkerToken])
		assert.Len(t, tp.overrides, 4, "only the token, gateway address, app name and caddy.enabled are passed: no worker name, no domain")
	})

	t.Run("the local worker has no Caddy", func(t *testing.T) {
		tp := &fakeTemplates{}

		_, err := prepareValues(tp, workertypes.OpenshiftWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				Token:       "tok",
				GatewayAddr: "catalog-worker-gateway-ns.apps.example.com:443",
			},
		})

		require.NoError(t, err)
		assert.NotContains(t, tp.overrides, workerconstants.ArgParamCaddyEnabled)
	})
}
