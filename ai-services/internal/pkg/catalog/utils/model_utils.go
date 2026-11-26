package utils

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
)

// ModelHelmReleaseNameRegex matches a model Helm release name: "<componentType>-<first 8 chars of componentID>".
// e.g. "llm-2b4410e6", "embedding-c08f9a8b", "reranker-a1b2c3d4".
var ModelHelmReleaseNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+-[0-9a-fA-F]{8}$`)

// ModelsNamespace returns the Kubernetes namespace for all local models deployed on OpenShift.
// Format: "ai-services-models". All models share this namespace, which is created during
// catalog configure and deleted during catalog uninstall.
func ModelsNamespace() string {
	return catalogconstants.CatalogModelsNamespace
}

// ModelHelmReleaseName builds the Helm release name for a local model component: "<componentType>-<first 8 chars of componentID>".
// e.g. "llm-2b4410e6", "embedding-c08f9a8b", "reranker-a1b2c3d4".
func ModelHelmReleaseName(componentID uuid.UUID, componentType string) string {
	return strings.ReplaceAll(componentType, "_", "-") + "-" + componentID.String()[:8]
}

// IsModelHelmReleaseName reports whether the given release name matches the model Helm release name pattern.
func IsModelHelmReleaseName(releaseName string) bool {
	return ModelHelmReleaseNameRegex.MatchString(releaseName)
}
