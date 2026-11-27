package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	commonconstants "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// CatalogAuthSecretName is the secret that holds the catalog auth settings shared by every
// service on a worker (Podman) or in an application namespace (OpenShift). Service pods read it
// through envFrom, so each key is an environment variable name.
const CatalogAuthSecretName = commonconstants.CatalogAuthSecret

// CatalogCABundleSecretName is the OpenShift Secret that holds the PEM CA bundle services use to
// verify the catalog. Pods mount it as a file; the PEM is never an environment variable.
const CatalogCABundleSecretName = "catalog-ca-bundle"

// catalogCABundleSecretKey is the key of the PEM inside CatalogCABundleSecretName.
const catalogCABundleSecretKey = "ca-bundle.crt"

// EnsurePodmanCatalogAuthSecret creates, or replaces, the shared catalog auth secret on the worker
// behind store. Podman has no persistent ConfigMap, and `kube play` can read a secret created
// beforehand, so the settings that are the same for every service are stored once here instead of
// being copied into each service's values.
//
// The secret holds only public addresses, no credential. Only the audience, which differs per
// service, stays in the service's own pod template. A pod reads the secret at start, so a changed
// address reaches a service when it is redeployed.
func EnsurePodmanCatalogAuthSecret(ctx context.Context, store SecretStore, isRemoteWorker bool) error {
	if err := store.CreateSecret(ctx, CatalogAuthSecretName, podmanCatalogAuthData(isRemoteWorker), nil); err != nil {
		return fmt.Errorf("failed to store catalog auth secret: %w", err)
	}

	return nil
}

// EnsureOpenShiftCatalogAuthSecret creates, or replaces, the shared catalog auth secret in the
// application namespace behind store, so every service chart reads the same addresses through
// envFrom, as on Podman. It also stores the catalog CA bundle in its own Secret when the catalog
// environment provides one; the charts mount that Secret as a file.
//
// The catalog auth secret holds only public addresses, no credential. A pod reads it at start,
// so a changed address reaches a service when it is redeployed.
func EnsureOpenShiftCatalogAuthSecret(ctx context.Context, store SecretStore, isRemoteWorker bool) error {
	data, err := openShiftCatalogAuthData(ctx, isRemoteWorker)
	if err != nil {
		return err
	}
	if err := store.CreateSecret(ctx, CatalogAuthSecretName, data, nil); err != nil {
		return fmt.Errorf("failed to store catalog auth secret: %w", err)
	}

	if ca := catalogCABundleFromEnv(); ca != "" {
		if err := store.CreateSecret(ctx, CatalogCABundleSecretName, map[string][]byte{catalogCABundleSecretKey: []byte(ca)}, nil); err != nil {
			return errors.New("failed to store catalog CA bundle secret")
		}
	}

	return nil
}

// openShiftCatalogAuthData returns the keys of the shared secret. A key without a value is left
// out, so a pod never sees an empty variable.
func openShiftCatalogAuthData(ctx context.Context, isRemoteWorker bool) (map[string][]byte, error) {
	var baseURL, plaintextHost, uiURL string

	if isRemoteWorker {
		// A remote cluster reaches the catalog on its public address, over HTTPS. The browser
		// is sent to the catalog UI address, which CATALOG_PUBLIC_UI_URL overrides.
		publicURL, err := resolveOpenShiftPublicCatalogURL(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve catalog public URL for remote worker: %w", err)
		}
		baseURL = publicURL

		uiURL = utils.GetEnv("CATALOG_PUBLIC_UI_URL", "")
		if uiURL == "" {
			uiURL = catalogUIURLFromAPIURL(publicURL)
		}
	} else {
		// A service in the catalog's cluster reaches the catalog on its Service DNS name, and
		// its UI infers the sign-in address from its own host name.
		plaintextHost = fmt.Sprintf("catalog-api.%s.svc.cluster.local", constants.CatalogAppName)
		baseURL = fmt.Sprintf("http://%s:8080", plaintextHost)
	}

	data := map[string][]byte{
		"AUTH_JWKS_URL":        []byte(baseURL + "/.well-known/jwks.json"),
		"AUTH_CATALOG_API_URL": []byte(baseURL),
		// The service UIs use this name for the sign-in code exchange.
		"CATALOG_API_URL": []byte(baseURL),
	}
	if plaintextHost != "" {
		data["AUTH_JWKS_PLAINTEXT_HOSTS"] = []byte(plaintextHost)
	}
	if uiURL != "" {
		data["CATALOG_UI_URL"] = []byte(uiURL)
	}

	return data, nil
}

// podmanCatalogAuthData returns the keys of the shared secret. A key without a value is left out,
// so a pod never sees an empty variable.
func podmanCatalogAuthData(isRemoteWorker bool) map[string][]byte {
	baseURL, plaintextHost, uiURL := podmanCatalogAddresses(isRemoteWorker)

	data := map[string][]byte{
		"AUTH_JWKS_URL":             []byte(baseURL + "/.well-known/jwks.json"),
		"AUTH_CATALOG_API_URL":      []byte(baseURL),
		"AUTH_JWKS_PLAINTEXT_HOSTS": []byte(plaintextHost),
		// The service UIs use these names for the sign-in code exchange.
		"CATALOG_API_URL": []byte(baseURL),
	}
	if uiURL != "" {
		data["CATALOG_UI_URL"] = []byte(uiURL)
	}

	return data
}

// podmanCatalogAddresses returns the catalog API address a service on the worker uses, the host
// that may serve JWKS over plain HTTP, and the catalog UI address a browser is sent to for sign-in
// (empty when it is unknown, in which case the UI infers it from its own host name).
func podmanCatalogAddresses(isRemoteWorker bool) (baseURL, plaintextHost, uiURL string) {
	if isRemoteWorker {
		// The catalog pod name only resolves on the catalog host's podman network, so a remote
		// worker reaches the catalog through its own Caddy egress server. The worker gateway
		// registers that route (catalog-auth) when the worker connects; Caddy forwards the request
		// over mTLS to the catalog's private ingress, which exposes only the service-auth
		// endpoints. The service-to-Caddy hop is plain HTTP inside the worker host, so the Caddy
		// host is also a plaintext host. The browser cannot use the internal route, so the sign-in
		// page keeps its public address.
		caddyHost := workerconstants.WorkerCaddyPodName
		baseURL = fmt.Sprintf("http://%s:%s%s", caddyHost, proxy.PrivateEgressPort, workerconstants.CatalogAuthPathPrefix)

		uiURL = utils.GetEnv("CATALOG_PUBLIC_UI_URL", "")
		if uiURL == "" {
			uiURL = podmanCatalogUIURL()
		}

		return baseURL, caddyHost, uiURL
	}

	// A service on the catalog host reaches the catalog directly on the podman network, and its
	// UI infers the sign-in address from its own host name.
	catalogHost := fmt.Sprintf("%s--catalog", constants.CatalogAppName)
	baseURL = fmt.Sprintf("http://%s:8080", catalogHost)

	return baseURL, catalogHost, ""
}
