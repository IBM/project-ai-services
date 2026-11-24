package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectServiceAuthValues_Podman(t *testing.T) {
	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", false)
	require.NoError(t, err)

	assert.Equal(t, "digitize", values["authAudience"])
	assert.Equal(t, "http://ai-services--catalog:8080/.well-known/jwks.json", values["authJwksUrl"])
	assert.Equal(t, "http://ai-services--catalog:8080", values["authCatalogApiUrl"])
	assert.Equal(t, "ai-services--catalog", values["authJwksPlaintextHosts"])
}

func TestInjectServiceAuthValues_Podman_Remote(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_HTTPS_PORT", "8443")

	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", true)
	require.NoError(t, err)

	assert.Equal(t, "https://catalog-api.example.com:8443/.well-known/jwks.json", values["authJwksUrl"])
	assert.Equal(t, "https://catalog-api.example.com:8443", values["authCatalogApiUrl"])
	assert.Equal(t, "https://catalog-ui.example.com:8443", values["authCatalogUiUrl"])
	assert.Equal(t, "", values["authJwksPlaintextHosts"])
}

func TestInjectServiceAuthValues_Podman_Remote_CatalogUIURL(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")

	// Services without a UI must not get the value (their schemas reject unknown keys).
	values := map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "similarity", values, "podman", true))
	assert.NotContains(t, values, "authCatalogUiUrl")

	// An explicit env override wins over the derived value.
	t.Setenv("CATALOG_PUBLIC_UI_URL", "https://ui.custom.example.com")
	values = map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.Equal(t, "https://ui.custom.example.com", values["authCatalogUiUrl"])
}

func TestInjectServiceAuthValues_Podman_Remote_NoPublicURL(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "")
	t.Setenv("CATALOG_PUBLIC_API_URL", "")

	err := InjectServiceAuthValues(context.Background(), "digitize", map[string]any{}, "podman", true)
	require.Error(t, err)
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

func TestInjectServiceAuthValues_Podman_Remote_CaddyRootCA(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_ADMIN_URL", "http://caddy:2019")
	orig := fetchCaddyRootCA
	t.Cleanup(func() { fetchCaddyRootCA = orig })

	fetchCaddyRootCA = func(context.Context, string) (string, error) { return "ROOT-PEM", nil }

	values := map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.Equal(t, "ROOT-PEM", values["authCatalogCaBundle"])

	// An explicit bundle is never replaced.
	values = map[string]any{"authCatalogCaBundle": "OPERATOR-PEM"}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.Equal(t, "OPERATOR-PEM", values["authCatalogCaBundle"])

	// A lookup failure does not fail the deployment and leaves the bundle empty.
	fetchCaddyRootCA = func(context.Context, string) (string, error) { return "", errors.New("boom") }
	values = map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.NotContains(t, values, "authCatalogCaBundle")

	// A local worker shares the catalog's network and never needs it.
	fetchCaddyRootCA = func(context.Context, string) (string, error) { return "ROOT-PEM", nil }
	values = map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", false))
	assert.NotContains(t, values, "authCatalogCaBundle")
}

func TestInjectServiceAuthValues_PreservesExplicitValues(t *testing.T) {
	values := map[string]any{
		"authJwksUrl":            "https://custom-auth.example.com/jwks",
		"authCatalogApiUrl":     "https://custom-auth.example.com",
		"authAudience":          "custom-aud",
		"authJwksPlaintextHosts": "custom-host",
		"authCatalogCaBundle":    "---PEM---",
	}

	err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", false)
	require.NoError(t, err)

	assert.Equal(t, "custom-aud", values["authAudience"])
	assert.Equal(t, "https://custom-auth.example.com/jwks", values["authJwksUrl"])
	assert.Equal(t, "https://custom-auth.example.com", values["authCatalogApiUrl"])
	assert.Equal(t, "custom-host", values["authJwksPlaintextHosts"])
	assert.Equal(t, "---PEM---", values["authCatalogCaBundle"])
}

func TestInjectServiceAuthValues_CABundleFromEnv(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	values := map[string]any{}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", false)
	require.NoError(t, err)

	assert.Equal(t, "CERT_PEM_CONTENT", values["authCatalogCaBundle"])
}

// The injector is generic: the audience is the catalog ID of whichever service is deployed.
func TestInjectServiceAuthValues_DigitalAssistantServices(t *testing.T) {
	for _, svc := range []string{"chat", "similarity", "summarize"} {
		t.Run(svc+"/podman", func(t *testing.T) {
			values := map[string]any{}
			require.NoError(t, InjectServiceAuthValues(context.Background(), svc, values, "podman", false))

			assert.Equal(t, svc, values["authAudience"])
			assert.Equal(t, "http://ai-services--catalog:8080/.well-known/jwks.json", values["authJwksUrl"])
			assert.Equal(t, "http://ai-services--catalog:8080", values["authCatalogApiUrl"])
			assert.Equal(t, "ai-services--catalog", values["authJwksPlaintextHosts"])
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

