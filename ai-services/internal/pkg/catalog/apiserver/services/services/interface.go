// Package services provides service-level helpers for catalog API server operations,
// including runtime-specific resolution of plain http:// internal URLs for deployed
// service pods. Additional service-level utilities should be added to this package.
package services

import (
	"fmt"
	"path"

	"github.com/google/uuid"
	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// resolver derives a plain http:// internal URL for a deployed service pod,
// avoiding TLS entirely for server-side calls from the catalog backend.
//
// Both runtimes register the public "api" endpoint URL in the DB using https://
// (self-signed cert on Podman via Caddy; edge-terminated route on OpenShift).
// resolver provides a per-runtime strategy that derives an equivalent internal
// http:// URL from the catalog template files and existing DB data:
//
//   - Podman    : http://<podname>:<container-port>          (shared host network)
//   - OpenShift : http://<svc>.<namespace>.svc.cluster.local:<port>  (in-cluster)
//
// Use newResolver to obtain the correct implementation for the active runtime.
type resolver interface {
	// internalURL derives a plain http:// URL for the service.
	//
	//   provider      – catalog provider used to look up the item FS and path.
	//   catalogID     – the catalog item ID for the service.
	//   apiURL        – the stored "api"-type URL from the DB endpoints JSON.
	//   applicationID – used by the OpenShift implementation to derive the
	//                   Kubernetes namespace.
	//
	// Returns "" when derivation fails; callers should fall back to apiURL.
	internalURL(provider *catalogpkg.CatalogProvider, catalogID, apiURL string, applicationID uuid.UUID) string
}

// newResolver returns the resolver for the given runtime type.
// Mirrors newRuntimeSync in the sync package — returns an error for unsupported
// runtime types so the caller fails fast at startup rather than silently at
// call time.
func newResolver(rt runtimetypes.RuntimeType) (resolver, error) {
	switch rt {
	case runtimetypes.RuntimeTypePodman:
		return &podmanResolver{}, nil
	case runtimetypes.RuntimeTypeOpenShift:
		return &openshiftResolver{}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime type for service URL resolver: %s", rt)
	}
}

// catalogTemplatePath returns the runtime-specific templates directory for a
// catalog item. It is used by both runtime resolvers to locate template files.
func catalogTemplatePath(provider *catalogpkg.CatalogProvider, catalogID, runtime string) (string, error) {
	servicePath, err := provider.GetCatalogItemPath(catalogID)
	if err != nil {
		return "", err
	}

	return path.Join(servicePath, runtime, "templates"), nil
}

// GetServiceURL returns the plain http:// internal URL for the named catalog
// service's "api" endpoint, bypassing the public https:// URL stored in the DB.
//
// Falls back to apiURL when any step fails (unsupported runtime, missing catalog
// item, no matching template) so all call sites degrade gracefully.
func GetServiceURL(provider *catalogpkg.CatalogProvider, catalogID, apiURL string, applicationID uuid.UUID) string {
	res, err := newResolver(vars.RuntimeFactory.GetRuntimeType())
	if err != nil {
		return apiURL
	}

	if internal := res.internalURL(provider, catalogID, apiURL, applicationID); internal != "" {
		return internal
	}

	return apiURL
}
