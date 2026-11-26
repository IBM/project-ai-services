package auth

import (
	"context"
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

// A service on a remote Podman worker reaches the catalog through its worker's Caddy egress
// (plain HTTP in, mTLS out), so it needs no public catalog-api address and no CA bundle.
func TestInjectServiceAuthValues_Podman_Remote(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_HTTPS_PORT", "8443")

	// A service with a UI declares authCatalogUiUrl in its values.yaml.
	values := map[string]any{"authCatalogUiUrl": ""}
	err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", true)
	require.NoError(t, err)

	assert.Equal(t, "http://ai-services--caddy:8080/catalog-auth/.well-known/jwks.json", values["authJwksUrl"])
	assert.Equal(t, "http://ai-services--caddy:8080/catalog-auth", values["authCatalogApiUrl"])
	assert.Equal(t, "ai-services--caddy", values["authJwksPlaintextHosts"])
	assert.Equal(t, "https://catalog-ui.example.com:8443", values["authCatalogUiUrl"])
	assert.NotContains(t, values, "authCatalogCaBundle")
}

func TestInjectServiceAuthValues_Podman_Remote_NeedsNoPublicCatalogAPI(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "")
	t.Setenv("CATALOG_PUBLIC_API_URL", "")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")

	values := map[string]any{"authCatalogUiUrl": ""}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "digitize", values, "podman", true))

	assert.Equal(t, "http://ai-services--caddy:8080/catalog-auth", values["authCatalogApiUrl"])
	// Without a domain the UI falls back to inferring the sign-in address from its own host.
	assert.Equal(t, "", values["authCatalogUiUrl"])
}

func TestInjectServiceAuthValues_Podman_Remote_CatalogUIURL(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_HTTPS_PORT", "443")

	// Services that do not declare authCatalogUiUrl must not get it (their schemas reject unknown keys).
	values := map[string]any{}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "similarity", values, "podman", true))
	assert.NotContains(t, values, "authCatalogUiUrl")

	// The default HTTPS port is omitted from the address.
	values = map[string]any{"authCatalogUiUrl": ""}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.Equal(t, "https://catalog-ui.example.com", values["authCatalogUiUrl"])

	// An explicit env override wins over the derived value.
	t.Setenv("CATALOG_PUBLIC_UI_URL", "https://ui.custom.example.com")
	values = map[string]any{"authCatalogUiUrl": ""}
	require.NoError(t, InjectServiceAuthValues(context.Background(), "chat", values, "podman", true))
	assert.Equal(t, "https://ui.custom.example.com", values["authCatalogUiUrl"])
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

func TestInjectServiceAuthValues_PreservesExplicitValues(t *testing.T) {
	for _, remote := range []bool{false, true} {
		values := map[string]any{
			"authJwksUrl":            "https://custom-auth.example.com/jwks",
			"authCatalogApiUrl":      "https://custom-auth.example.com",
			"authAudience":           "custom-aud",
			"authJwksPlaintextHosts": "custom-host",
		}

		err := InjectServiceAuthValues(context.Background(), "digitize", values, "podman", remote)
		require.NoError(t, err)

		assert.Equal(t, "custom-aud", values["authAudience"])
		assert.Equal(t, "https://custom-auth.example.com/jwks", values["authJwksUrl"])
		assert.Equal(t, "https://custom-auth.example.com", values["authCatalogApiUrl"])
		assert.Equal(t, "custom-host", values["authJwksPlaintextHosts"])
	}
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
