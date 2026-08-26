package validate

import (
	"archive/tar"
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	k8syaml "sigs.k8s.io/yaml"
)

// Podman runtime directory constant.
const podmanRuntime = "podman"

// PodmanBundleValidator validates that a bundle archive contains the required
// Podman runtime layout:
//
//	<root>/
//	└── podman/
//	    ├── metadata.yaml     (required: name, version, resources.cpu, resources.memory)
//	    ├── values.yaml       (required)
//	    ├── values.schema.json (required)
//	    └── templates/
//	        └── *.yaml.tmpl   (at least one required)
type PodmanBundleValidator struct{}

// NewPodmanBundleValidator returns a new PodmanBundleValidator.
func NewPodmanBundleValidator() *PodmanBundleValidator {
	return &PodmanBundleValidator{}
}

// Validate checks that the archive contains a valid Podman runtime sub-directory.
// If no podman/ directory is present the validator returns nil early — the bundle
// simply does not target the Podman runtime.
// When podman/ is present, Validate runs two checks in order:
//  1. Structural: all required files must exist.
//  2. Semantic: podman/metadata.yaml is parsed, required fields are validated, and
//     its version is checked against rootVersion (the root metadata.yaml version).
func (v *PodmanBundleValidator) Validate(archiveBytes []byte, topDir, rootVersion string) error {
	found, err := collectPodmanPaths(archiveBytes, topDir)
	if err != nil {
		return err
	}
	if !found.runtimeDirSeen {
		return nil // bundle does not target the Podman runtime
	}

	if err := validatePodmanPaths(found); err != nil {
		return err
	}

	if err := validateValuesSchema(found.schemaBytes, "podman/values.schema.json"); err != nil {
		return err
	}

	if err := validatePodmanTemplates(found.templFiles); err != nil {
		return err
	}

	return bundlemetadata.ValidatePodmanMetadata(found.metadataBytes, rootVersion)
}

// podmanPaths tracks the presence of files required by the Podman layout and
// carries the raw bytes of files needed for semantic validation.
type podmanPaths struct {
	runtimeDirSeen bool // set on the first entry under podman/
	hasMetadata    bool
	hasValues      bool
	hasSchema      bool
	hasTemplFile   bool
	metadataBytes  []byte            // raw content of podman/metadata.yaml
	schemaBytes    []byte            // raw content of podman/values.schema.json
	templFiles     map[string][]byte // name → raw content of each templates/*.yaml.tmpl
}

// collectPodmanPaths walks the archive once, records which required Podman paths
// exist, and captures the raw bytes of podman/metadata.yaml.
func collectPodmanPaths(archiveBytes []byte, topDir string) (*podmanPaths, error) {
	found := &podmanPaths{}

	err := scanEntriesWithContent(archiveBytes, func(name string, hdr *tar.Header, content []byte) (bool, error) {
		rel := stripTopDir(name, topDir)
		if !strings.HasPrefix(rel, podmanRuntime+"/") {
			return false, nil
		}
		found.runtimeDirSeen = true
		found.recordEntry(strings.TrimPrefix(rel, podmanRuntime+"/"), hdr, content)

		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}

// recordEntry updates found based on the archive entry sub-path (relative to podman/).
func (found *podmanPaths) recordEntry(sub string, hdr *tar.Header, content []byte) {
	if hdr.Typeflag == tar.TypeDir {
		return
	}
	switch {
	case sub == "metadata.yaml":
		found.hasMetadata = true
		found.metadataBytes = content
	case sub == "values.yaml":
		found.hasValues = true
	case sub == "values.schema.json":
		found.hasSchema = true
		found.schemaBytes = content
	case strings.HasPrefix(sub, "templates/") && strings.HasSuffix(sub, ".yaml.tmpl"):
		found.hasTemplFile = true
		if found.templFiles == nil {
			found.templFiles = make(map[string][]byte)
		}
		found.templFiles[sub] = content
	}
}

// validatePodmanPaths returns a ValidationError when any required Podman path is absent.
func validatePodmanPaths(found *podmanPaths) error {
	missing := collectMissingPodmanFiles(found)
	if len(missing) == 0 {
		return nil
	}

	return &validators.ValidationError{
		Code: http.StatusUnprocessableEntity,
		Message: fmt.Sprintf(
			"podman runtime layout is invalid: missing required file(s): %s",
			strings.Join(missing, ", "),
		),
	}
}

// collectMissingPodmanFiles returns the list of descriptive names for absent required files.
func collectMissingPodmanFiles(found *podmanPaths) []string {
	var missing []string

	if !found.hasMetadata {
		missing = append(missing, "podman/metadata.yaml")
	}
	if !found.hasValues {
		missing = append(missing, "podman/values.yaml")
	}
	if !found.hasSchema {
		missing = append(missing, "podman/values.schema.json")
	}
	if !found.hasTemplFile {
		missing = append(missing, "podman/templates/*.yaml.tmpl (at least one)")
	}

	return missing
}


// validatePodmanTemplates parses each *.yaml.tmpl with text/template (the same
// package the runtime uses) to surface syntax errors, then renders each template
// with nil data (missingkey=zero so all .Field references expand to "") and
// checks the required fields of every ai-services Podman pod spec.
func validatePodmanTemplates(templFiles map[string][]byte) error {
	for path, content := range templFiles {
		// 1. Parse the Go template — catches syntax errors.
		tmpl, err := template.New(path).Option("missingkey=zero").Parse(string(content))
		if err != nil {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("podman/%s: template syntax error: %s", path, err),
			}
		}

		// 2. Execute with nil data; all .Field references expand to "".
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, nil); err != nil {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("podman/%s: template execution error: %s", path, err),
			}
		}

		// 3. Unmarshal rendered YAML into a generic map and check required keys.
		var doc map[string]any
		if err := k8syaml.Unmarshal(buf.Bytes(), &doc); err != nil {
			return &validators.ValidationError{
				Code:    http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("podman/%s: rendered output is not valid YAML: %s", path, err),
			}
		}

		if err := checkPodmanTemplateSpec(doc, path); err != nil {
			return err
		}
	}

	return nil
}

// checkPodmanTemplateSpec verifies that the rendered template document has the
// required fields every ai-services Podman pod spec must declare.
//
// Required top-level fields:
//   - apiVersion  (non-blank string)
//   - kind        (non-blank string)
//
// Required metadata fields:
//   - metadata.name                                        (non-blank string)
//   - metadata.labels[constants.ApplicationTemplateKey]   (key must be present;
//     the value is a runtime expression so it is not checked here)
func checkPodmanTemplateSpec(doc map[string]any, path string) error {
	missing := make([]string, 0, 4)

	if v, _ := doc["apiVersion"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "apiVersion")
	}
	if v, _ := doc["kind"].(string); strings.TrimSpace(v) == "" {
		missing = append(missing, "kind")
	}

	templateLabelEntry := fmt.Sprintf("metadata.labels[%q]", constants.ApplicationTemplateKey)
	meta, _ := doc["metadata"].(map[string]any)
	if meta == nil {
		missing = append(missing, "metadata.name")
		missing = append(missing, templateLabelEntry)
	} else {
		if v, _ := meta["name"].(string); strings.TrimSpace(v) == "" {
			missing = append(missing, "metadata.name")
		}
		labels, _ := meta["labels"].(map[string]any)
		if _, ok := labels[constants.ApplicationTemplateKey]; !ok {
			missing = append(missing, templateLabelEntry)
		}
	}

	if len(missing) > 0 {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"podman/%s: missing required Kubernetes spec field(s): %s",
				path, strings.Join(missing, ", "),
			),
		}
	}

	return nil
}


// Ensure PodmanBundleValidator implements BundleValidator at compile time.
var _ BundleValidator = (*PodmanBundleValidator)(nil)
