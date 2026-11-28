package openshift

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"
	"sigs.k8s.io/yaml"

	"github.com/project-ai-services/ai-services/assets"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
)

// render renders the chart under dir (in the given embedded FS) with the values overlaid on the
// chart defaults, and returns the rendered manifests keyed by template path.
func render(t *testing.T, worker bool, dir string, overlay map[string]any) map[string]string {
	t.Helper()

	fsys := assets.CatalogFS
	if worker {
		fsys = assets.WorkerFS
	}

	chrt, err := catalogutils.LoadChartFromFS(fsys, dir)
	require.NoError(t, err)

	vals, err := util.ToRenderValues(chrt, overlay, common.ReleaseOptions{Name: "ai-services", Namespace: "ns"}, nil)
	require.NoError(t, err)

	out, err := engine.Render(chrt, vals)
	require.NoError(t, err)

	return out
}

func find(out map[string]string, suffix string) (string, bool) {
	for k, v := range out {
		if strings.HasSuffix(k, suffix) {
			return v, true
		}
	}

	return "", false
}

func TestWorkerChartCaddy(t *testing.T) {
	t.Run("a named worker renders the Caddyfile into the ConfigMap", func(t *testing.T) {
		out := render(t, true, "worker/openshift", map[string]any{
			"caddy": map[string]any{"enabled": true},
		})

		cm, ok := find(out, "worker-caddy-configmap.yaml")
		require.True(t, ok)
		assert.Contains(t, cm, "admin 0.0.0.0:2019")
		assert.Contains(t, cm, "trusted_ca_cert_file /data/worker-tls/ca.crt")

		// The cluster assigns the Route host: the chart must not set one.
		route, ok := find(out, "worker-caddy-route.yaml")
		require.True(t, ok)
		assert.Contains(t, route, "termination: passthrough")
		assert.NotRegexp(t, `(?m)^\s*host:`, route, "the cluster assigns the host")

		w, ok := find(out, "worker.yaml")
		require.True(t, ok)
		assert.Contains(t, w, "CADDY_ADMIN_URL")
		assert.NotContains(t, w, "WORKER_NAME")
		assert.NotContains(t, w, "DOMAIN_SUFFIX")
	})

	t.Run("the local worker deploys no Caddy", func(t *testing.T) {
		out := render(t, true, "worker/openshift", nil)

		for _, f := range []string{"configmap", "route", "service", "pvc"} {
			body, _ := find(out, "worker-caddy-"+f+".yaml")
			assert.Empty(t, strings.TrimSpace(body), f)
		}

		w, ok := find(out, "worker.yaml")
		require.True(t, ok)
		assert.NotContains(t, w, "name: caddy")
		assert.NotContains(t, w, "CADDY_ADMIN_URL")
	})
}

func TestCatalogChartCaddyfile(t *testing.T) {
	out := render(t, false, "catalog/openshift", nil)

	cm, ok := find(out, "catalog-caddy-configmap.yaml")
	require.True(t, ok)
	assert.Contains(t, cm, "admin 0.0.0.0:2019")
	assert.Contains(t, cm, "reverse_proxy litellm:")
	assert.NotContains(t, cm, "{{")
}

// netpol is the part of a NetworkPolicy the tests inspect.
type netpol struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		PodSelector struct {
			MatchLabels      map[string]string `json:"matchLabels"`
			MatchExpressions []struct {
				Key      string   `json:"key"`
				Operator string   `json:"operator"`
				Values   []string `json:"values"`
			} `json:"matchExpressions"`
		} `json:"podSelector"`
		PolicyTypes []string `json:"policyTypes"`
		Ingress     []struct {
			From []struct {
				PodSelector *struct {
					MatchLabels map[string]string `json:"matchLabels"`
				} `json:"podSelector"`
				NamespaceSelector *struct {
					MatchLabels      map[string]string `json:"matchLabels"`
					MatchExpressions []struct {
						Key    string   `json:"key"`
						Values []string `json:"values"`
					} `json:"matchExpressions"`
				} `json:"namespaceSelector"`
			} `json:"from"`
			Ports []struct {
				Port int `json:"port"`
			} `json:"ports"`
		} `json:"ingress"`
	} `json:"spec"`
}

// policies parses every NetworkPolicy document in a rendered manifest.
func policies(t *testing.T, manifest string) []netpol {
	t.Helper()

	var out []netpol
	for _, doc := range strings.Split(manifest, "\n---\n") {
		if !strings.Contains(doc, "kind: NetworkPolicy") {
			continue
		}

		var np netpol
		require.NoError(t, yaml.Unmarshal([]byte(doc), &np))
		out = append(out, np)
	}

	return out
}

func TestWorkerChartNetworkPolicy(t *testing.T) {
	out := render(t, true, "worker/openshift", map[string]any{
		"caddy": map[string]any{
			"enabled":       true,
			"networkPolicy": map[string]any{"egressNamespaces": []any{"models"}},
		},
	})

	body, ok := find(out, "worker-caddy-networkpolicy.yaml")
	require.True(t, ok)

	nps := policies(t, body)
	require.Len(t, nps, 1)
	np := nps[0]

	assert.Equal(t, "ai-services--caddy", np.Spec.PodSelector.MatchLabels["ai-services.io/component"])
	assert.Equal(t, []string{"Ingress"}, np.Spec.PolicyTypes, "an Ingress policy that selects the pod denies all other ingress")

	byPort := map[int][]string{}
	for _, rule := range np.Spec.Ingress {
		for _, p := range rule.Ports {
			for _, from := range rule.From {
				if from.PodSelector != nil {
					continue
				}
				require.NotNil(t, from.NamespaceSelector, ":%d must be limited to namespaces, not open to all pods", p.Port)
				for k := range from.NamespaceSelector.MatchLabels {
					byPort[p.Port] = append(byPort[p.Port], k)
				}
				for _, e := range from.NamespaceSelector.MatchExpressions {
					byPort[p.Port] = append(byPort[p.Port], e.Values...)
				}
			}
		}
	}

	assert.ElementsMatch(t, []string{"ns", "models"}, byPort[8080], ":8080 is for the release namespace and the configured ones only")
	assert.ElementsMatch(t, []string{
		"policy-group.network.openshift.io/ingress",
		"policy-group.network.openshift.io/host-network",
	}, byPort[9443], ":9443 is for the router only")
	assert.NotContains(t, byPort, 2019, "the Admin API is reached by the worker pod only, a pod selector rather than a namespace")
	adminFrom := np.Spec.Ingress[0].From[0].PodSelector
	require.NotNil(t, adminFrom, ":2019 must be limited to the worker pod")
	assert.Equal(t, "worker", adminFrom.MatchLabels["ai-services.io/component"], ":2019 is for the worker pod only")

	t.Run("no policy without Caddy", func(t *testing.T) {
		out := render(t, true, "worker/openshift", nil)
		body, _ := find(out, "worker-caddy-networkpolicy.yaml")
		assert.Empty(t, strings.TrimSpace(body))
	})
}

func TestWorkerChartDeployment(t *testing.T) {
	out := render(t, true, "worker/openshift", map[string]any{"caddy": map[string]any{"enabled": true}})

	w, ok := find(out, "worker.yaml")
	require.True(t, ok)

	// Rolling update would leave the new pod waiting on the old one's ReadWriteOnce volume.
	assert.Regexp(t, `(?m)^  strategy:\n    type: Recreate`, w)

	// Caddy runs in its own pod, so the worker pod has no Caddy container.
	assert.NotContains(t, w, "- name: caddy\n")

	// The token is mounted for the worker container only, never for the Caddy pod.
	assert.Contains(t, w, "automountServiceAccountToken: false")
	assert.Equal(t, 1, strings.Count(w, "name: sa-token\n              mountPath"), "exactly one container mounts the token")

	c, ok := find(out, "worker-caddy-deployment.yaml")
	require.True(t, ok)
	assert.Regexp(t, `(?m)^  strategy:\n    type: Recreate`, c)
	assert.NotContains(t, c, "sa-token", "the Caddy pod has no service account token")

	// worker-tls is ReadWriteOnce: the Caddy pod must be scheduled on the worker's node.
	assert.Contains(t, c, "requiredDuringSchedulingIgnoredDuringExecution")
	assert.Contains(t, c, "ai-services.io/component: worker\n")
}

func TestCatalogChartNetworkPolicies(t *testing.T) {
	out := render(t, false, "catalog/openshift", nil)

	body, ok := find(out, "catalog-networkpolicy-default.yaml")
	require.True(t, ok)

	byName := map[string]netpol{}
	for _, np := range policies(t, body) {
		byName[np.Metadata.Name] = np
	}

	t.Run("the namespace denies ingress by default", func(t *testing.T) {
		deny, ok := byName["default-deny-ingress"]
		require.True(t, ok)
		assert.Empty(t, deny.Spec.PodSelector.MatchLabels)
		assert.Empty(t, deny.Spec.PodSelector.MatchExpressions, "an empty selector selects every pod")
		assert.Empty(t, deny.Spec.Ingress)
		assert.Equal(t, []string{"Ingress"}, deny.Spec.PolicyTypes)
	})

	t.Run("the allow rule never selects the control-plane Caddy", func(t *testing.T) {
		allow, ok := byName["allow-namespace-and-router"]
		require.True(t, ok)
		require.Len(t, allow.Spec.PodSelector.MatchExpressions, 1)
		e := allow.Spec.PodSelector.MatchExpressions[0]
		assert.Equal(t, "ai-services.io/component", e.Key)
		assert.Equal(t, "NotIn", e.Operator)
		assert.Equal(t, []string{"ai-services--caddy"}, e.Values,
			"policies are additive: selecting ai-services--caddy here would reopen :2019 to every pod")
	})

	t.Run("the Caddy policy gives :2019 to the backend alone and :9443 to the router", func(t *testing.T) {
		cb, ok := find(out, "catalog-caddy-networkpolicy.yaml")
		require.True(t, ok)
		assert.Contains(t, cb, "ai-services.io/component: catalog-backend")
		assert.Contains(t, cb, "policy-group.network.openshift.io/host-network")
	})
}

// TestCaddyResetsStaleAutosave guards the --resume upgrade path. With --resume Caddy loads its
// autosave instead of the Caddyfile once the autosave exists, so without this step a changed
// Caddyfile (a security fix, say) would never apply to an installed Caddy.
func TestCaddyResetsStaleAutosave(t *testing.T) {
	workerOut := render(t, true, "worker/openshift", map[string]any{"caddy": map[string]any{"enabled": true}})
	catalogOut := render(t, false, "catalog/openshift", nil)

	workerYAML, _ := find(workerOut, "worker-caddy-deployment.yaml")
	catalogYAML, _ := find(catalogOut, "catalog-caddy.yaml")
	cases := map[string]string{"worker": workerYAML, "catalog": catalogYAML}

	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, manifest)

			reset := strings.Index(manifest, "rm -f /config/caddy/autosave.json")
			start := strings.Index(manifest, "exec /tmp/caddy run")
			require.NotEqual(t, -1, reset, "no autosave reset")
			require.NotEqual(t, -1, start)
			assert.Less(t, reset, start, "the autosave must be dropped before Caddy starts")
			assert.Contains(t, manifest, "/config/caddy/Caddyfile.applied")
			assert.Contains(t, manifest, "--resume", "runtime-registered routes still survive a restart that changes nothing")
		})
	}
}

func TestCatalogChartCaddyHostAliases(t *testing.T) {
	t.Run("none by default", func(t *testing.T) {
		out := render(t, false, "catalog/openshift", nil)
		d, ok := find(out, "catalog-caddy.yaml")
		require.True(t, ok)
		assert.NotContains(t, d, "hostAliases")
	})

	t.Run("rendered into the Caddy pod when set", func(t *testing.T) {
		out := render(t, false, "catalog/openshift", map[string]any{
			"hostAliases": []any{map[string]any{"ip": "10.0.0.5", "hostnames": []any{"worker-caddy-ns.apps.lab.example.com"}}},
		})
		d, ok := find(out, "catalog-caddy.yaml")
		require.True(t, ok)
		assert.Contains(t, d, "hostAliases:")
		assert.Contains(t, d, `- ip: "10.0.0.5"`)
		assert.Contains(t, d, `- "worker-caddy-ns.apps.lab.example.com"`)
	})
}
