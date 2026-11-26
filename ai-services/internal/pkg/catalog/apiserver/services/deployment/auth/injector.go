package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// InjectServiceAuthValues populates auth-related values in a service's Values map if not already explicitly set.
// It handles:
// - authJwksUrl: URL to fetch the catalog JWKS keys
// - authCatalogApiUrl: base URL for the catalog API (used by UI exchange proxy)
// - authJwksPlaintextHosts: hosts allowed to serve JWKS over plain HTTP
// - authAudience: default audience if missing
// - authCatalogCaBundle (OpenShift only): CA bundle PEM if present in the catalog environment or already set.
//
// A Podman service never needs a CA bundle: a service on the catalog host reaches the catalog over
// the pod network, and a service on a remote worker reaches it through its worker's Caddy, which
// carries the request to the catalog over mTLS.
func InjectServiceAuthValues(
	ctx context.Context,
	serviceID string,
	values map[string]any,
	runtimeType string,
	isRemoteWorker bool,
) error {
	if values == nil {
		return nil
	}

	// Default audience is the service catalog ID if not set
	if _, ok := values["authAudience"]; !ok || values["authAudience"] == "" {
		values["authAudience"] = serviceID
	}

	// If authJwksUrl is already explicitly set by caller/operator, keep it
	existingJwksURL, _ := values["authJwksUrl"].(string)

	switch runtimeType {
	case runtimeTypes.RuntimeTypeOpenShift.String():
		if err := injectOpenShiftAuth(ctx, values, existingJwksURL, isRemoteWorker); err != nil {
			return err
		}

		// Propagate CA bundle from environment if not set in values
		if _, ok := values["authCatalogCaBundle"]; !ok || values["authCatalogCaBundle"] == "" {
			if envCA := utils.GetEnv("AUTH_CATALOG_CA_BUNDLE", ""); envCA != "" {
				values["authCatalogCaBundle"] = envCA
			}
		}
	case runtimeTypes.RuntimeTypePodman.String():
		injectPodmanAuth(values, existingJwksURL, isRemoteWorker)
	}

	return nil
}

// CredentialProvisioner creates and revokes the per-deployment service credentials (DA2).
type CredentialProvisioner interface {
	// ProvisionWithGrants creates a credential for caller in a deployment with exactly the
	// given grants (audience -> roles) and returns it. It is returned once and never logged.
	ProvisionWithGrants(ctx context.Context, caller, deploymentID string, grants map[string][]string) (string, error)
	// RevokeForDeployment revokes every credential of a deployment, for a deployment that
	// failed or a service that was deleted.
	RevokeForDeployment(ctx context.Context, deploymentID string) error
}

func injectOpenShiftAuth(ctx context.Context, values map[string]any, existingJwksURL string, isRemoteWorker bool) error {
	if !isRemoteWorker {
		injectCoLocatedOpenShiftAuth(values, existingJwksURL)

		return nil
	}

	return injectRemoteOpenShiftAuth(ctx, values, existingJwksURL)
}

func injectCoLocatedOpenShiftAuth(values map[string]any, existingJwksURL string) {
	clusterDNS := fmt.Sprintf("catalog-api.%s.svc.cluster.local:8080", constants.CatalogAppName)
	if existingJwksURL == "" {
		values["authJwksUrl"] = fmt.Sprintf("http://%s/.well-known/jwks.json", clusterDNS)
	}
	if _, ok := values["authCatalogApiUrl"]; !ok || values["authCatalogApiUrl"] == "" {
		values["authCatalogApiUrl"] = fmt.Sprintf("http://%s", clusterDNS)
	}
	if _, ok := values["authJwksPlaintextHosts"]; !ok || values["authJwksPlaintextHosts"] == "" {
		values["authJwksPlaintextHosts"] = fmt.Sprintf("catalog-api.%s.svc.cluster.local", constants.CatalogAppName)
	}
}

func injectRemoteOpenShiftAuth(ctx context.Context, values map[string]any, existingJwksURL string) error {
	publicURL, err := resolveOpenShiftPublicCatalogURL(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve catalog public URL for remote worker: %w", err)
	}
	if existingJwksURL == "" {
		values["authJwksUrl"] = fmt.Sprintf("%s/.well-known/jwks.json", publicURL)
	}
	if _, ok := values["authCatalogApiUrl"]; !ok || values["authCatalogApiUrl"] == "" {
		values["authCatalogApiUrl"] = publicURL
	}
	injectCatalogUIURL(values, catalogUIURLFromAPIURL(publicURL))
	if _, ok := values["authJwksPlaintextHosts"]; !ok {
		values["authJwksPlaintextHosts"] = ""
	}

	return nil
}

func injectPodmanAuth(values map[string]any, existingJwksURL string, isRemoteWorker bool) {
	if isRemoteWorker {
		// The catalog pod name only resolves on the catalog host's podman network, so a remote
		// worker reaches the catalog through its own Caddy (plain HTTP in, mTLS out).
		injectRemotePodmanAuth(values, existingJwksURL)

		return
	}

	// On Podman, local worker accesses catalog API directly on the podman network
	catalogHost := fmt.Sprintf("%s--catalog", constants.CatalogAppName)
	catalogPort := "8080"
	if existingJwksURL == "" {
		values["authJwksUrl"] = fmt.Sprintf("http://%s:%s/.well-known/jwks.json", catalogHost, catalogPort)
	}
	if _, ok := values["authCatalogApiUrl"]; !ok || values["authCatalogApiUrl"] == "" {
		values["authCatalogApiUrl"] = fmt.Sprintf("http://%s:%s", catalogHost, catalogPort)
	}
	if _, ok := values["authJwksPlaintextHosts"]; !ok || values["authJwksPlaintextHosts"] == "" {
		values["authJwksPlaintextHosts"] = catalogHost
	}
}

// injectRemotePodmanAuth points a service on a remote Podman worker at the catalog through the
// worker's own Caddy egress server. The worker gateway registers that route (catalog-auth) when the
// worker connects; Caddy forwards the request over mTLS to the catalog's private ingress, which
// exposes only the service-auth endpoints. The service-to-Caddy hop is plain HTTP inside the worker
// host, so the Caddy host is also listed as a plaintext host.
func injectRemotePodmanAuth(values map[string]any, existingJwksURL string) {
	caddyHost := workerconstants.WorkerCaddyPodName
	baseURL := fmt.Sprintf("http://%s:%s%s", caddyHost, proxy.PrivateEgressPort, workerconstants.CatalogAuthPathPrefix)

	if existingJwksURL == "" {
		values["authJwksUrl"] = baseURL + "/.well-known/jwks.json"
	}
	if _, ok := values["authCatalogApiUrl"]; !ok || values["authCatalogApiUrl"] == "" {
		values["authCatalogApiUrl"] = baseURL
	}
	if _, ok := values["authJwksPlaintextHosts"]; !ok || values["authJwksPlaintextHosts"] == "" {
		values["authJwksPlaintextHosts"] = caddyHost
	}

	// The browser cannot use the internal route, so the sign-in page keeps its public address.
	injectCatalogUIURL(values, podmanCatalogUIURL())
}

// injectCatalogUIURL sets authCatalogUiUrl, the catalog UI address a service UI sends the
// browser to for sign-in. A service UI on another host cannot infer it from its own address.
// Only a service whose values.yaml declares authCatalogUiUrl (it has a UI) gets the value;
// the other services' schemas reject unknown keys. CATALOG_PUBLIC_UI_URL wins; otherwise
// derivedURL is used. If neither is set it is left unset and the UI falls back to inferring
// it from its own host name.
func injectCatalogUIURL(values map[string]any, derivedURL string) {
	current, declared := values["authCatalogUiUrl"]
	if !declared {
		return
	}
	if current != "" {
		return
	}
	if envURL := utils.GetEnv("CATALOG_PUBLIC_UI_URL", ""); envURL != "" {
		values["authCatalogUiUrl"] = envURL

		return
	}
	if derivedURL != "" {
		values["authCatalogUiUrl"] = derivedURL
	}
}

// catalogUIURLFromAPIURL derives the catalog UI address from the public catalog API address
// (catalog-api.<domain> -> catalog-ui.<domain>). It returns "" when the API address does not
// have that shape.
func catalogUIURLFromAPIURL(publicAPIURL string) string {
	if !strings.Contains(publicAPIURL, "://catalog-api.") {
		return ""
	}

	return strings.Replace(publicAPIURL, "://catalog-api.", "://catalog-ui.", 1)
}

// podmanCatalogUIURL returns the public catalog UI address on a Podman catalog host
// (catalog-ui.<DOMAIN_SUFFIX>[:<CADDY_HTTPS_PORT>]), or "" when DOMAIN_SUFFIX is not set.
func podmanCatalogUIURL() string {
	domainSuffix := utils.GetEnv("DOMAIN_SUFFIX", "")
	if domainSuffix == "" {
		return ""
	}

	port := utils.GetEnv("CADDY_HTTPS_PORT", "443")
	if port == "443" || port == "" {
		return fmt.Sprintf("https://catalog-ui.%s", domainSuffix)
	}

	return fmt.Sprintf("https://catalog-ui.%s:%s", domainSuffix, port)
}

func resolveOpenShiftPublicCatalogURL(ctx context.Context) (string, error) {
	// First check if an explicit public URL is provided via environment
	if envURL := utils.GetEnv("CATALOG_PUBLIC_API_URL", ""); envURL != "" {
		return envURL, nil
	}

	// Look up catalog-api OpenShift route
	host, err := catalogutils.CatalogAPIRouteHost(ctx)
	if err != nil {
		// Fallback to DOMAIN_SUFFIX if available
		domainSuffix := utils.GetEnv("DOMAIN_SUFFIX", "")
		if domainSuffix != "" {
			port := utils.GetEnv("CADDY_HTTPS_PORT", "443")
			if port == "443" || port == "" {
				return fmt.Sprintf("https://catalog-api.%s", domainSuffix), nil
			}

			return fmt.Sprintf("https://catalog-api.%s:%s", domainSuffix, port), nil
		}

		return "", err
	}

	return fmt.Sprintf("https://%s", host), nil
}
