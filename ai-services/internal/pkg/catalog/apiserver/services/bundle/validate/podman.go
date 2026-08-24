package validate

import (
	"archive/tar"
	"fmt"
	"net/http"
	"strings"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
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
	topDir, err := resolveTopDir(archiveBytes, topDir)
	if err != nil {
		return err
	}

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

	return bundlemetadata.ValidatePodmanMetadata(found.metadataBytes, rootVersion)
}

// podmanPaths tracks the presence of files required by the Podman layout and
// carries the raw bytes of podman/metadata.yaml for semantic validation.
type podmanPaths struct {
	runtimeDirSeen bool // set on the first entry under podman/
	hasMetadata    bool
	hasValues      bool
	hasSchema      bool
	hasTemplFile   bool
	metadataBytes  []byte // raw content of podman/metadata.yaml
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
	case strings.HasPrefix(sub, "templates/") && strings.HasSuffix(sub, ".yaml.tmpl"):
		found.hasTemplFile = true
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

// Ensure PodmanBundleValidator implements BundleValidator at compile time.
var _ BundleValidator = (*PodmanBundleValidator)(nil)
