package auth

import (
	"context"
	"fmt"
	"strings"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// InjectServiceAuthValues populates auth-related values in a service's Values map if not already explicitly set.
// It handles:
//   - authAudience: default audience if missing
//   - authCatalogCaBundleSecret (OpenShift only): name of the Secret that holds the catalog CA bundle,
//     set when the catalog environment has one. The PEM itself never enters values.
//
// The catalog addresses (JWKS URL, catalog API and UI URLs, plaintext hosts) are the same for every
// service on a worker or in an application, so they are not values. EnsurePodmanCatalogAuthSecret
// and EnsureOpenShiftCatalogAuthSecret store them once in a shared "catalog-auth" secret that the
// pods read through envFrom.
func InjectServiceAuthValues(serviceID string, values map[string]any, runtimeType string) {
	if values == nil {
		return
	}

	// Default audience is the service catalog ID if not set
	if _, ok := values["authAudience"]; !ok || values["authAudience"] == "" {
		values["authAudience"] = serviceID
	}

	if runtimeType == runtimeTypes.RuntimeTypeOpenShift.String() && catalogCABundleFromEnv() != "" {
		// Always overwrite: a value supplied in the request must not point the pod at another Secret.
		values["authCatalogCaBundleSecret"] = CatalogCABundleSecretName
	}
}

// catalogCABundleFromEnv returns the PEM CA bundle the catalog environment provides for services
// that verify a catalog served over a private CA, or "".
func catalogCABundleFromEnv() string {
	return utils.GetEnv("AUTH_CATALOG_CA_BUNDLE", "")
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
