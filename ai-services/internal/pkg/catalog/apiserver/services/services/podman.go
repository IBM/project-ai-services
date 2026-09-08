package services

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/google/uuid"
	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// podmanResolver derives http://<podname>:<port> for Podman deployments.
//
// Podman registers service endpoints through Caddy; the stored "api" URL is
// https://<podname>.<domain-suffix>. The pod name is the first DNS label of
// that hostname and is directly reachable from the catalog-backend over the
// shared host network (no TLS needed).
// The container port is read from the ai-services.io/routes annotation in the
// pod template file: "port:subdomain:type,..." selecting the "api"-typed entry.
type podmanResolver struct{}

func (r *podmanResolver) internalURL(provider *catalogpkg.CatalogProvider, catalogID, apiURL string, _ uuid.UUID) string {
	podHost := firstDNSLabel(apiURL)
	if podHost == "" {
		return ""
	}

	catalogPath, err := catalogTemplatePath(provider, catalogID, runtimetypes.RuntimeTypePodman.String())
	if err != nil {
		return ""
	}

	itemFS, err := provider.GetItemFS(catalogID)
	if err != nil {
		return ""
	}

	port := podmanAPIPortFromTemplates(itemFS, catalogPath)
	if port == "" {
		return ""
	}

	return fmt.Sprintf("http://%s:%s", podHost, port)
}

// firstDNSLabel returns the first label of the URL's hostname.
// For "https://digitize-backend-abc123.10.0.0.1.nip.io" it returns "digitize-backend-abc123".
func firstDNSLabel(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}

	const splitInTwo = 2

	return strings.SplitN(u.Hostname(), ".", splitInTwo)[0]
}

// podmanAPIPortFromTemplates scans .tmpl files in catalogPath for the
// ai-services.io/routes annotation and returns the port for the "api" entry.
// Annotation format: "port:subdomain:type, ...".
// Example: "4001:digitize-ui-abc:ui,4000:digitize-backend-abc:api" returns "4000".
func podmanAPIPortFromTemplates(itemFS fs.FS, catalogPath string) string {
	const annotationKey = "ai-services.io/routes:"

	var port string

	_ = fs.WalkDir(itemFS, catalogPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || port != "" {
			return nil
		}

		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		data, readErr := fs.ReadFile(itemFS, path)
		if readErr != nil {
			return nil
		}

		port = apiPortFromRoutesAnnotation(string(data), annotationKey)

		return nil
	})

	return port
}

// apiPortFromRoutesAnnotation finds the ai-services.io/routes line in raw
// template text and returns the port for the "api"-typed route entry.
func apiPortFromRoutesAnnotation(content, annotationKey string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, annotationKey) {
			continue
		}

		// Value may be quoted: ai-services.io/routes: "4000:pod:api,..."
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, annotationKey))
		value = strings.Trim(value, `"'`)

		for _, entry := range strings.Split(value, ",") {
			parts := strings.Split(strings.TrimSpace(entry), ":")
			// format: port:subdomain:type — exactly three colon-separated fields
			const routePartCount = 3
			if len(parts) != routePartCount {
				continue
			}

			if strings.TrimSpace(parts[2]) == "api" {
				return strings.TrimSpace(parts[0])
			}
		}
	}

	return ""
}

// Ensure podmanResolver implements resolver at compile time.
var _ resolver = (*podmanResolver)(nil)
