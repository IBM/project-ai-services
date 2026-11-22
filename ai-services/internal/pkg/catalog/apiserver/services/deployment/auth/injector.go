package auth

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// InjectServiceAuthValues populates auth-related values in a service's Values map if not already explicitly set.
// It handles:
// - authJwksUrl: URL to fetch the catalog JWKS keys
// - authCatalogApiUrl: base URL for the catalog API (used by UI exchange proxy)
// - authJwksPlaintextHosts: hosts allowed to serve JWKS over plain HTTP
// - authAudience: default audience if missing
// - authCatalogCaBundle: CA bundle PEM if present in the catalog environment or already set.
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

	var err error
	switch runtimeType {
	case runtimeTypes.RuntimeTypeOpenShift.String():
		err = injectOpenShiftAuth(ctx, values, existingJwksURL, isRemoteWorker)
	case runtimeTypes.RuntimeTypePodman.String():
		injectPodmanAuth(values, existingJwksURL)
	}
	if err != nil {
		return err
	}

	// Propagate CA bundle from environment if not set in values
	if _, ok := values["authCatalogCaBundle"]; !ok || values["authCatalogCaBundle"] == "" {
		if envCA := utils.GetEnv("AUTH_CATALOG_CA_BUNDLE", ""); envCA != "" {
			values["authCatalogCaBundle"] = envCA
		}
	}

	return nil
}

// CredentialProvisioner creates the per-deployment service credential (DA2) for a service
// that needs one. It returns an empty string, and no error, for a service that needs none.
type CredentialProvisioner interface {
	ProvisionIfNeeded(ctx context.Context, serviceID, deploymentID string) (string, error)
}

// InjectServiceCredential puts a fresh service credential into values as authServiceCredential
// for a service that needs one (today only chat). The templates deliver it to the pod as a
// secret. A nil provisioner leaves values unchanged, so a deployment without credential
// support behaves as before. The value must never be logged.
func InjectServiceCredential(
	ctx context.Context,
	serviceID, deploymentID string,
	values map[string]any,
	p CredentialProvisioner,
) error {
	if values == nil || p == nil {
		return nil
	}

	credential, err := p.ProvisionIfNeeded(ctx, serviceID, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to provision service credential for %s: %w", serviceID, err)
	}
	if credential != "" {
		values["authServiceCredential"] = credential
	}

	return nil
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
	if _, ok := values["authJwksPlaintextHosts"]; !ok {
		values["authJwksPlaintextHosts"] = ""
	}

	return nil
}

func injectPodmanAuth(values map[string]any, existingJwksURL string) {
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
