package auth

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	commonconstants "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// CatalogAuthSecretName is the Podman secret that holds the catalog auth settings shared by every
// service on a worker. Service pods read it through envFrom, so each key is an environment
// variable name.
const CatalogAuthSecretName = commonconstants.CatalogAuthSecret

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
