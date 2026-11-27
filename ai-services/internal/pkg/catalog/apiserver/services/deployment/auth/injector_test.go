package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Only the audience is set per service; the catalog addresses live in the shared catalog-auth
// secret (see catalog_auth_secret_test.go).
func TestInjectServiceAuthValues_Podman(t *testing.T) {
	values := map[string]any{}
	InjectServiceAuthValues("digitize", values, "podman")

	assert.Equal(t, map[string]any{"authAudience": "digitize"}, values)
}

func TestInjectServiceAuthValues_OpenShift_NoCABundle(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "")

	values := map[string]any{}
	InjectServiceAuthValues("digitize", values, "openshift")

	assert.Equal(t, map[string]any{"authAudience": "digitize"}, values)
}

func TestInjectServiceAuthValues_OpenShift_CABundleIsReferencedByName(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	values := map[string]any{}
	InjectServiceAuthValues("digitize", values, "openshift")

	assert.Equal(t, CatalogCABundleSecretName, values["authCatalogCaBundleSecret"])
	for k, v := range values {
		assert.NotEqual(t, "CERT_PEM_CONTENT", v, "the PEM must never enter values (key %s)", k)
	}

	// A value supplied in the request must not point the pod at another Secret.
	values = map[string]any{"authCatalogCaBundleSecret": "someone-elses-secret"}
	InjectServiceAuthValues("digitize", values, "openshift")
	assert.Equal(t, CatalogCABundleSecretName, values["authCatalogCaBundleSecret"])
}

func TestInjectServiceAuthValues_Podman_NeverInjectsCABundle(t *testing.T) {
	// Even with a bundle in the environment, a Podman service gets none: it never verifies a
	// catalog certificate.
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	values := map[string]any{}
	InjectServiceAuthValues("chat", values, "podman")
	assert.NotContains(t, values, "authCatalogCaBundleSecret")
}

func TestInjectServiceAuthValues_PreservesExplicitAudience(t *testing.T) {
	values := map[string]any{"authAudience": "custom-aud"}
	InjectServiceAuthValues("digitize", values, "openshift")

	assert.Equal(t, "custom-aud", values["authAudience"])
}

func TestInjectServiceAuthValues_NilValues(t *testing.T) {
	assert.NotPanics(t, func() { InjectServiceAuthValues("digitize", nil, "openshift") })
}

// The injector is generic: the audience is the catalog ID of whichever service is deployed.
func TestInjectServiceAuthValues_DigitalAssistantServices(t *testing.T) {
	for _, svc := range []string{"chat", "similarity", "summarize"} {
		for _, rt := range []string{"podman", "openshift"} {
			values := map[string]any{}
			InjectServiceAuthValues(svc, values, rt)

			assert.Equal(t, svc, values["authAudience"], "%s/%s", svc, rt)
		}
	}
}
