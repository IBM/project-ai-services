package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On Podman only the audience is set per service; the catalog addresses live in the shared
// catalog-auth secret (see catalog_auth_secret_test.go).
func TestInjectServiceAuthValues_Podman(t *testing.T) {
	for _, remote := range []bool{false, true} {
		values := map[string]any{}
		require.NoError(t, InjectServiceAuthValues(context.Background(), "digitize", values, "podman", remote))

		assert.Equal(t, map[string]any{"authAudience": "digitize"}, values, "remote=%v", remote)
	}
}

func TestInjectServiceAuthValues_OpenShift_CoLocated(t *testing.T) {
	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "openshift", false)
	require.NoError(t, err)

	assert.Equal(t, "digitize", values["authAudience"])
	assert.Equal(t, "http://catalog-api.ai-services.svc.cluster.local:8080/.well-known/jwks.json", values["authJwksUrl"])
	assert.Equal(t, "http://catalog-api.ai-services.svc.cluster.local:8080", values["authCatalogApiUrl"])
	assert.Equal(t, "catalog-api.ai-services.svc.cluster.local", values["authJwksPlaintextHosts"])
}

func TestInjectServiceAuthValues_OpenShift_Remote_WithDomainSuffix(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "apps.cluster.example.com")
	t.Setenv("CADDY_HTTPS_PORT", "443")

	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "openshift", true)
	require.NoError(t, err)

	assert.Equal(t, "digitize", values["authAudience"])
	assert.Equal(t, "https://catalog-api.apps.cluster.example.com/.well-known/jwks.json", values["authJwksUrl"])
	assert.Equal(t, "https://catalog-api.apps.cluster.example.com", values["authCatalogApiUrl"])
	assert.Equal(t, "", values["authJwksPlaintextHosts"])
}

func TestInjectServiceAuthValues_Podman_NeverInjectsCABundle(t *testing.T) {
	// Even with a bundle in the environment, a Podman service gets none: it never verifies a
	// catalog certificate.
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	for _, remote := range []bool{false, true} {
		values := map[string]any{}
		require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", remote))
		assert.NotContains(t, values, "authCatalogCaBundle", "remote=%v", remote)
	}
}

func TestInjectServiceAuthValues_OpenShift_PreservesExplicitValues(t *testing.T) {
	values := map[string]any{
		"authJwksUrl":            "https://custom-auth.example.com/jwks",
		"authCatalogApiUrl":      "https://custom-auth.example.com",
		"authAudience":           "custom-aud",
		"authJwksPlaintextHosts": "custom-host",
	}

	err := InjectServiceAuthValues(context.Background(), "digitize", values, "openshift", false)
	require.NoError(t, err)

	assert.Equal(t, "custom-aud", values["authAudience"])
	assert.Equal(t, "https://custom-auth.example.com/jwks", values["authJwksUrl"])
	assert.Equal(t, "https://custom-auth.example.com", values["authCatalogApiUrl"])
	assert.Equal(t, "custom-host", values["authJwksPlaintextHosts"])
}

func TestInjectServiceAuthValues_OpenShift_CABundleFromEnv(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "openshift", false)
	require.NoError(t, err)
	assert.Equal(t, "CERT_PEM_CONTENT", values["authCatalogCaBundle"])

	// An explicit bundle is never replaced.
	values = map[string]any{"authCatalogCaBundle": "---PEM---"}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "digitize", values, "openshift", false))
	assert.Equal(t, "---PEM---", values["authCatalogCaBundle"])
}

// The injector is generic: the audience is the catalog ID of whichever service is deployed.
func TestInjectServiceAuthValues_DigitalAssistantServices(t *testing.T) {
	for _, svc := range []string{"chat", "similarity", "summarize"} {
		t.Run(svc+"/podman", func(t *testing.T) {
			values := map[string]any{}
			require.NoError(t, InjectServiceAuthValues(context.Background(), svc, values, "podman", false))

			assert.Equal(t, svc, values["authAudience"])
		})

		t.Run(svc+"/openshift", func(t *testing.T) {
			values := map[string]any{}
			require.NoError(t, InjectServiceAuthValues(context.Background(), svc, values, "openshift", false))

			assert.Equal(t, svc, values["authAudience"])
			assert.Equal(t, "http://catalog-api.ai-services.svc.cluster.local:8080/.well-known/jwks.json", values["authJwksUrl"])
			assert.Equal(t, "catalog-api.ai-services.svc.cluster.local", values["authJwksPlaintextHosts"])
		})
	}
}
