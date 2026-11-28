package worker

import (
	"testing"

	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metaRegistry is a WorkerRegistry that only knows worker metadata.
type metaRegistry struct {
	stream.WorkerRegistry
	meta     map[string]map[string]string
	runtimes map[string]string
}

func (m metaRegistry) WorkerMetadata(name string) (map[string]string, bool) {
	meta, ok := m.meta[name]

	return meta, ok
}

func (m metaRegistry) WorkerRuntimeType(name string) (string, bool) {
	rt, ok := m.runtimes[name]

	return rt, ok
}

func TestPrivateIngressHost(t *testing.T) {
	reg := metaRegistry{
		meta: map[string]map[string]string{
			"pw": {workerconstants.MetaKeyDomainSuffix: "example.com"},
			"ow": {workerconstants.MetaKeyPrivateIngressHost: "worker-caddy-ns.apps.example.com"},
			"nr": {},
		},
		runtimes: map[string]string{"pw": "podman", "ow": "openshift", "nr": "openshift"},
	}

	t.Run("podman worker is dialed at <name>.<suffix>", func(t *testing.T) {
		got, err := NewResolver(reg).PrivateIngressHost("pw")
		require.NoError(t, err)
		assert.Equal(t, "pw.example.com", got)
	})

	t.Run("openshift worker is dialed at the route host it reported", func(t *testing.T) {
		got, err := NewResolver(reg).PrivateIngressHost("ow")
		require.NoError(t, err)
		assert.Equal(t, "worker-caddy-ns.apps.example.com", got)
	})

	t.Run("an openshift worker that reported no route host is an error", func(t *testing.T) {
		_, err := NewResolver(reg).PrivateIngressHost("nr")
		require.ErrorContains(t, err, "did not report its Caddy route host")
	})

	t.Run("a podman worker that reported no domain suffix is an error", func(t *testing.T) {
		reg := metaRegistry{
			meta:     map[string]map[string]string{"p": {}},
			runtimes: map[string]string{"p": "podman"},
		}
		_, err := NewResolver(reg).PrivateIngressHost("p")
		require.ErrorContains(t, err, "did not report a domain suffix")
	})

	t.Run("an unknown worker is an error", func(t *testing.T) {
		_, err := NewResolver(reg).PrivateIngressHost("nope")
		require.Error(t, err)
	})
}

func TestDomainSuffix(t *testing.T) {
	reg := metaRegistry{meta: map[string]map[string]string{
		"w1": {
			workerconstants.MetaKeyDomainSuffix:       "example.com",
			workerconstants.MetaKeyPrivateIngressPort: "8443",
		},
		"w2": {},
	}}

	t.Run("returns the suffix the worker reported", func(t *testing.T) {
		got, err := NewResolver(reg).DomainSuffix("w1")
		require.NoError(t, err)
		assert.Equal(t, "example.com", got)
	})

	t.Run("a worker that reported none is an error, not an empty dial name", func(t *testing.T) {
		_, err := NewResolver(reg).DomainSuffix("w2")
		require.ErrorContains(t, err, "did not report a domain suffix")
	})

	t.Run("a worker that is not connected", func(t *testing.T) {
		_, err := NewResolver(reg).DomainSuffix("nope")
		require.ErrorContains(t, err, "not connected")
	})

	t.Run("no registry", func(t *testing.T) {
		_, err := NewResolver(nil).DomainSuffix("w1")
		require.Error(t, err)
	})

	t.Run("returns private ingress port if set", func(t *testing.T) {
		port := NewResolver(reg).PrivateIngressPort("w1")
		assert.Equal(t, "8443", port)
	})

	t.Run("falls back to default port if not set or worker unknown", func(t *testing.T) {
		assert.Equal(t, "9443", NewResolver(reg).PrivateIngressPort("w2"))
		assert.Equal(t, "9443", NewResolver(reg).PrivateIngressPort("nope"))
		assert.Equal(t, "9443", NewResolver(nil).PrivateIngressPort("w1"))
	})
}
