package services

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/google/uuid"
	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"sigs.k8s.io/yaml"
)

// openshiftResolver derives http://<svc>.<namespace>.svc.cluster.local:<port>
// for OpenShift deployments.
//
// The K8s Service name and targetPort are read from the route YAML template
// file whose ai-services.io/endpoint-type label equals "api". The namespace
// is derived from the applicationID via catalogutils.AppNamespace.
type openshiftResolver struct{}

func (r *openshiftResolver) internalURL(provider *catalogpkg.CatalogProvider, catalogID, _ string, applicationID uuid.UUID) string {
	catalogPath, err := catalogTemplatePath(provider, catalogID, runtimetypes.RuntimeTypeOpenShift.String())
	if err != nil {
		return ""
	}

	itemFS, err := provider.GetItemFS(catalogID)
	if err != nil {
		return ""
	}

	svcName, port := openshiftAPIServiceFromTemplates(itemFS, catalogPath)
	if svcName == "" || port == "" {
		return ""
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%s", svcName, catalogutils.AppNamespace(applicationID), port)
}

// routeYAML is the minimal struct needed to parse an OpenShift Route YAML file.
type routeYAML struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Port struct {
			TargetPort any `json:"targetPort"`
		} `json:"port"`
		To struct {
			Name string `json:"name"`
		} `json:"to"`
	} `json:"spec"`
}

// openshiftAPIServiceFromTemplates walks catalogPath for .yaml files, parses
// each as an OpenShift Route, and returns the K8s Service name and targetPort
// for the route whose ai-services.io/endpoint-type label equals "api".
func openshiftAPIServiceFromTemplates(itemFS fs.FS, catalogPath string) (svcName, port string) {
	_ = fs.WalkDir(itemFS, catalogPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || svcName != "" {
			return nil
		}

		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		data, readErr := fs.ReadFile(itemFS, path)
		if readErr != nil {
			return nil
		}

		var route routeYAML
		if unmarshalErr := yaml.Unmarshal(data, &route); unmarshalErr != nil {
			return nil
		}

		if route.Metadata.Labels["ai-services.io/endpoint-type"] != "api" {
			return nil
		}

		if route.Spec.To.Name == "" {
			return nil
		}

		svcName = route.Spec.To.Name
		port = fmt.Sprintf("%v", route.Spec.Port.TargetPort)

		return nil
	})

	return svcName, port
}

// Ensure openshiftResolver implements resolver at compile time.
var _ resolver = (*openshiftResolver)(nil)
