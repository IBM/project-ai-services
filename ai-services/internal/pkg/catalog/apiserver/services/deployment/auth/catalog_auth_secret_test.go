package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func catalogAuthData(t *testing.T, isRemote bool) map[string]string {
	t.Helper()

	store := newFakeStore()
	require.NoError(t, EnsurePodmanCatalogAuthSecret(context.Background(), store, isRemote))
	require.Contains(t, store.secrets, CatalogAuthSecretName)

	out := map[string]string{}
	for k, v := range store.secrets[CatalogAuthSecretName] {
		out[k] = string(v)
	}

	return out
}

// A service on the catalog host reaches the catalog directly on the podman network.
func TestEnsurePodmanCatalogAuthSecret_Local(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")

	assert.Equal(t, map[string]string{
		"AUTH_JWKS_URL":             "http://ai-services--catalog:8080/.well-known/jwks.json",
		"AUTH_CATALOG_API_URL":      "http://ai-services--catalog:8080",
		"CATALOG_API_URL":           "http://ai-services--catalog:8080",
		"AUTH_JWKS_PLAINTEXT_HOSTS": "ai-services--catalog",
	}, catalogAuthData(t, false))
}

// A service on a remote Podman worker reaches the catalog through its worker's Caddy egress
// (plain HTTP in, mTLS out), so it needs no public catalog-api address and no CA bundle.
func TestEnsurePodmanCatalogAuthSecret_Remote(t *testing.T) {
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_HTTPS_PORT", "8443")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")

	assert.Equal(t, map[string]string{
		"AUTH_JWKS_URL":             "http://ai-services--caddy:8080/catalog-auth/.well-known/jwks.json",
		"AUTH_CATALOG_API_URL":      "http://ai-services--caddy:8080/catalog-auth",
		"CATALOG_API_URL":           "http://ai-services--caddy:8080/catalog-auth",
		"AUTH_JWKS_PLAINTEXT_HOSTS": "ai-services--caddy",
		"CATALOG_UI_URL":            "https://catalog-ui.example.com:8443",
	}, catalogAuthData(t, true))
}

func TestEnsurePodmanCatalogAuthSecret_Remote_UIURL(t *testing.T) {
	// Without a domain the UI infers the sign-in address from its own host, so the key is absent.
	t.Setenv("DOMAIN_SUFFIX", "")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")
	assert.NotContains(t, catalogAuthData(t, true), "CATALOG_UI_URL")

	// The default HTTPS port is omitted from the address.
	t.Setenv("DOMAIN_SUFFIX", "example.com")
	t.Setenv("CADDY_HTTPS_PORT", "443")
	assert.Equal(t, "https://catalog-ui.example.com", catalogAuthData(t, true)["CATALOG_UI_URL"])

	// An explicit env override wins over the derived value.
	t.Setenv("CATALOG_PUBLIC_UI_URL", "https://ui.custom.example.com")
	assert.Equal(t, "https://ui.custom.example.com", catalogAuthData(t, true)["CATALOG_UI_URL"])
}

func TestEnsurePodmanCatalogAuthSecret_StoreError(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("boom")

	err := EnsurePodmanCatalogAuthSecret(context.Background(), store, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog auth secret")
}

func openShiftCatalogAuth(t *testing.T, isRemote bool) (map[string]string, *fakeStore) {
	t.Helper()

	store := newFakeStore()
	require.NoError(t, EnsureOpenShiftCatalogAuthSecret(context.Background(), store, isRemote))
	require.Contains(t, store.secrets, CatalogAuthSecretName)

	out := map[string]string{}
	for k, v := range store.secrets[CatalogAuthSecretName] {
		out[k] = string(v)
	}

	return out, store
}

// A service in the catalog's cluster reaches the catalog on its Service DNS name.
func TestEnsureOpenShiftCatalogAuthSecret_CoLocated(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "")

	data, store := openShiftCatalogAuth(t, false)

	assert.Equal(t, map[string]string{
		"AUTH_JWKS_URL":             "http://catalog-api.ai-services.svc.cluster.local:8080/.well-known/jwks.json",
		"AUTH_CATALOG_API_URL":      "http://catalog-api.ai-services.svc.cluster.local:8080",
		"CATALOG_API_URL":           "http://catalog-api.ai-services.svc.cluster.local:8080",
		"AUTH_JWKS_PLAINTEXT_HOSTS": "catalog-api.ai-services.svc.cluster.local",
	}, data)
	assert.NotContains(t, store.secrets, CatalogCABundleSecretName, "no bundle in the environment")
}

// A remote cluster reaches the catalog on its public HTTPS address, so no host is plaintext.
func TestEnsureOpenShiftCatalogAuthSecret_Remote(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "")
	t.Setenv("CATALOG_PUBLIC_API_URL", "https://catalog-api.apps.cluster.example.com")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")

	data, _ := openShiftCatalogAuth(t, true)

	assert.Equal(t, map[string]string{
		"AUTH_JWKS_URL":        "https://catalog-api.apps.cluster.example.com/.well-known/jwks.json",
		"AUTH_CATALOG_API_URL": "https://catalog-api.apps.cluster.example.com",
		"CATALOG_API_URL":      "https://catalog-api.apps.cluster.example.com",
		"CATALOG_UI_URL":       "https://catalog-ui.apps.cluster.example.com",
	}, data)
}

func TestEnsureOpenShiftCatalogAuthSecret_Remote_UIURLOverride(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "")
	t.Setenv("CATALOG_PUBLIC_API_URL", "https://catalog-api.apps.cluster.example.com")
	t.Setenv("CATALOG_PUBLIC_UI_URL", "https://ui.custom.example.com")

	data, _ := openShiftCatalogAuth(t, true)
	assert.Equal(t, "https://ui.custom.example.com", data["CATALOG_UI_URL"])

	// An API address that is not catalog-api.<domain> gives no derived UI address.
	t.Setenv("CATALOG_PUBLIC_UI_URL", "")
	t.Setenv("CATALOG_PUBLIC_API_URL", "https://catalog.example.com")
	data, _ = openShiftCatalogAuth(t, true)
	assert.NotContains(t, data, "CATALOG_UI_URL")
}

func TestEnsureOpenShiftCatalogAuthSecret_StoresCABundleInItsOwnSecret(t *testing.T) {
	t.Setenv("AUTH_CATALOG_CA_BUNDLE", "CERT_PEM_CONTENT")

	data, store := openShiftCatalogAuth(t, false)

	assert.Equal(t, map[string][]byte{"ca-bundle.crt": []byte("CERT_PEM_CONTENT")}, store.secrets[CatalogCABundleSecretName])
	for k, v := range data {
		assert.NotContains(t, v, "CERT_PEM_CONTENT", "the PEM must not be an environment variable (key %s)", k)
	}
}

func TestEnsureOpenShiftCatalogAuthSecret_StoreError(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("boom")

	err := EnsureOpenShiftCatalogAuthSecret(context.Background(), store, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog auth secret")
}
