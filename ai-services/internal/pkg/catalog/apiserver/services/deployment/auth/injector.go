package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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
		err = injectOpenShiftAuth(ctx, serviceID, values, existingJwksURL, isRemoteWorker)
	case runtimeTypes.RuntimeTypePodman.String():
		err = injectPodmanAuth(serviceID, values, existingJwksURL, isRemoteWorker)
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

	// A remote Podman worker reaches the catalog over HTTPS through Caddy. With "tls internal"
	// Caddy's certificate is signed by its own local CA, which the worker does not trust.
	if runtimeType == runtimeTypes.RuntimeTypePodman.String() && isRemoteWorker {
		injectCaddyRootCA(ctx, values)
	}

	return nil
}

// fetchCaddyRootCA reads Caddy's internal root CA. It is a variable so tests can replace it.
var fetchCaddyRootCA = utils.GetCaddyInternalRootCA

// injectCaddyRootCA sets authCatalogCaBundle to Caddy's internal root CA when no bundle was
// supplied. It is best effort: on failure the value stays empty, TLS verification stays on,
// and the service fails closed until the operator supplies a bundle.
func injectCaddyRootCA(ctx context.Context, values map[string]any) {
	if v, ok := values["authCatalogCaBundle"]; ok && v != "" {
		return
	}
	adminURL := utils.GetEnv("CADDY_ADMIN_URL", "")
	if adminURL == "" {
		return
	}
	root, err := fetchCaddyRootCA(ctx, adminURL)
	if err != nil {
		logger.WarningfCtx(ctx, "Could not read the Caddy root CA for a remote service; set AUTH_CATALOG_CA_BUNDLE: %v\n", err)

		return
	}
	if root != "" {
		values["authCatalogCaBundle"] = root
	}
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

func injectOpenShiftAuth(ctx context.Context, serviceID string, values map[string]any, existingJwksURL string, isRemoteWorker bool) error {
	if !isRemoteWorker {
		injectCoLocatedOpenShiftAuth(values, existingJwksURL)

		return nil
	}

	return injectRemoteOpenShiftAuth(ctx, serviceID, values, existingJwksURL)
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

func injectRemoteOpenShiftAuth(ctx context.Context, serviceID string, values map[string]any, existingJwksURL string) error {
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
	injectCatalogUIURL(serviceID, values, publicURL)
	if _, ok := values["authJwksPlaintextHosts"]; !ok {
		values["authJwksPlaintextHosts"] = ""
	}

	return nil
}

func injectPodmanAuth(serviceID string, values map[string]any, existingJwksURL string, isRemoteWorker bool) error {
	if isRemoteWorker {
		// The catalog pod name only resolves on the catalog host's podman network, so a
		// remote worker must use the catalog's public (HTTPS) API URL instead.
		return injectRemotePodmanAuth(serviceID, values, existingJwksURL)
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

	return nil
}

func injectRemotePodmanAuth(serviceID string, values map[string]any, existingJwksURL string) error {
	publicURL, err := resolvePodmanPublicCatalogURL()
	if err != nil {
		return fmt.Errorf("failed to resolve catalog public URL for remote worker: %w", err)
	}
	if existingJwksURL == "" {
		values["authJwksUrl"] = fmt.Sprintf("%s/.well-known/jwks.json", publicURL)
	}
	if _, ok := values["authCatalogApiUrl"]; !ok || values["authCatalogApiUrl"] == "" {
		values["authCatalogApiUrl"] = publicURL
	}
	injectCatalogUIURL(serviceID, values, publicURL)
	if _, ok := values["authJwksPlaintextHosts"]; !ok {
		values["authJwksPlaintextHosts"] = ""
	}

	return nil
}

// servicesWithUI lists the services whose UI redirects the browser to the catalog for sign-in.
var servicesWithUI = map[string]bool{"chat": true, "digitize": true}

// injectCatalogUIURL sets authCatalogUiUrl, the catalog UI address a service UI sends the
// browser to for sign-in. A service UI on another host cannot infer it from its own address.
// CATALOG_PUBLIC_UI_URL wins; otherwise it is derived from the public API URL
// (catalog-api.<domain> -> catalog-ui.<domain>). If neither works it is left unset and the
// UI falls back to inferring it from its own host name.
func injectCatalogUIURL(serviceID string, values map[string]any, publicAPIURL string) {
	if !servicesWithUI[serviceID] {
		return
	}
	if v, ok := values["authCatalogUiUrl"]; ok && v != "" {
		return
	}
	if envURL := utils.GetEnv("CATALOG_PUBLIC_UI_URL", ""); envURL != "" {
		values["authCatalogUiUrl"] = envURL

		return
	}
	if strings.Contains(publicAPIURL, "://catalog-api.") {
		values["authCatalogUiUrl"] = strings.Replace(publicAPIURL, "://catalog-api.", "://catalog-ui.", 1)
	}
}

func resolvePodmanPublicCatalogURL() (string, error) {
	if envURL := utils.GetEnv("CATALOG_PUBLIC_API_URL", ""); envURL != "" {
		return envURL, nil
	}

	domainSuffix := utils.GetEnv("DOMAIN_SUFFIX", "")
	if domainSuffix == "" {
		return "", fmt.Errorf("neither CATALOG_PUBLIC_API_URL nor DOMAIN_SUFFIX is set")
	}
	port := utils.GetEnv("CADDY_HTTPS_PORT", "443")
	if port == "443" || port == "" {
		return fmt.Sprintf("https://catalog-api.%s", domainSuffix), nil
	}

	return fmt.Sprintf("https://catalog-api.%s:%s", domainSuffix, port), nil
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
