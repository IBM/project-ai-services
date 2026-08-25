package validate

import (
	"archive/tar"
	"fmt"
	"net/http"
	"strings"

	bundlemetadata "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"helm.sh/helm/v4/pkg/chart/common"
	chartcommonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
)

// OpenShift runtime directory constant.
const openShiftRuntime = "openshift"

// OpenShiftBundleValidator validates that a bundle archive contains the required
// OpenShift runtime layout:
//
//	<root>/
//	└── openshift/
//	    ├── metadata.yaml     (required: name, version, resources.cpu, resources.memory)
//	    ├── Chart.yaml        (required)
//	    ├── values.yaml       (required)
//	    ├── values.schema.json (required)
//	    └── templates/
//	        └── *.yaml        (at least one required)
type OpenShiftBundleValidator struct{}

// NewOpenShiftBundleValidator returns a new OpenShiftBundleValidator.
func NewOpenShiftBundleValidator() *OpenShiftBundleValidator {
	return &OpenShiftBundleValidator{}
}

// Validate checks that the archive contains a valid OpenShift runtime sub-directory.
// If no openshift/ directory is present the validator returns nil early — the bundle
// simply does not target the OpenShift runtime.
// When openshift/ is present, Validate runs two passes over the archive:
//  1. collectOpenShiftPaths — one gzip/tar stream that records structural presence,
//     captures openshift/metadata.yaml and Chart.yaml bytes for semantic checks, and
//     also builds the []*archive.BufferedFile slice needed for Helm loading.
//  2. Helm validation — loads the in-memory file slice (no further archive scan) and
//     validates in two stages:
//     a. chart.Validate() — checks Chart.yaml metadata (apiVersion, name, semver version, type).
//     b. engine.Render()  — renders all templates against default values to surface
//     Go template syntax errors and undefined variable references.
//
// The Chart.yaml version equality check is done inside helmValidateOpenShift after
// loader.LoadFiles populates chrt.Metadata.Version, removing the need for a separate
// ValidateOpenShiftChartVersion YAML decode.
func (v *OpenShiftBundleValidator) Validate(archiveBytes []byte, topDir, rootVersion string) error {
	found, err := collectOpenShiftPaths(archiveBytes, topDir)
	if err != nil {
		return err
	}
	if !found.runtimeDirSeen {
		return nil // bundle does not target the OpenShift runtime
	}

	if err := validateOpenShiftPaths(found); err != nil {
		return err
	}

	if err := bundlemetadata.ValidateOpenShiftMetadata(found.metadataBytes, rootVersion); err != nil {
		return err
	}

	return helmValidateOpenShift(found.bufferedFiles, rootVersion)
}

// openShiftPaths tracks the presence of files required by the OpenShift layout,
// carries the raw bytes of openshift/metadata.yaml for semantic validation, and
// accumulates the []*archive.BufferedFile slice used by Helm loading — all
// collected in a single archive scan.
type openShiftPaths struct {
	runtimeDirSeen bool // set on the first entry under openshift/
	hasMetadata    bool
	hasChart       bool
	hasValues      bool
	hasSchema      bool
	hasTemplFile   bool
	metadataBytes  []byte                  // raw content of openshift/metadata.yaml
	bufferedFiles  []*archive.BufferedFile // all openshift/ files, for loader.LoadFiles
}

// collectOpenShiftPaths walks the archive once, records which required OpenShift paths
// exist, and captures the raw bytes of openshift/metadata.yaml.
func collectOpenShiftPaths(archiveBytes []byte, topDir string) (*openShiftPaths, error) {
	found := &openShiftPaths{}

	err := scanEntriesWithContent(archiveBytes, func(name string, hdr *tar.Header, content []byte) (bool, error) {
		rel := stripTopDir(name, topDir)
		if !strings.HasPrefix(rel, openShiftRuntime+"/") {
			return false, nil
		}
		found.runtimeDirSeen = true
		found.recordEntry(strings.TrimPrefix(rel, openShiftRuntime+"/"), hdr, content)

		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}

// recordEntry updates found based on the archive entry sub-path (relative to openshift/).
// Every regular file is also appended to bufferedFiles for Helm loading.
func (found *openShiftPaths) recordEntry(sub string, hdr *tar.Header, content []byte) {
	if hdr.Typeflag == tar.TypeDir {
		return
	}
	switch {
	case sub == "metadata.yaml":
		found.hasMetadata = true
		found.metadataBytes = content
	case sub == "Chart.yaml":
		found.hasChart = true
	case sub == "values.yaml":
		found.hasValues = true
	case sub == "values.schema.json":
		found.hasSchema = true
	case strings.HasPrefix(sub, "templates/") && strings.HasSuffix(sub, ".yaml"):
		found.hasTemplFile = true
	}
	// Accumulate every regular file for Helm loading (eliminates collectOpenShiftBufferedFiles).
	found.bufferedFiles = append(found.bufferedFiles, &archive.BufferedFile{Name: sub, Data: content})
}

// validateOpenShiftPaths returns a ValidationError when any required OpenShift path is absent.
func validateOpenShiftPaths(found *openShiftPaths) error {
	missing := collectMissingOpenShiftFiles(found)
	if len(missing) == 0 {
		return nil
	}

	return &validators.ValidationError{
		Code: http.StatusUnprocessableEntity,
		Message: fmt.Sprintf(
			"openshift runtime layout is invalid: missing required file(s): %s",
			strings.Join(missing, ", "),
		),
	}
}

// collectMissingOpenShiftFiles returns the list of descriptive names for absent required files.
func collectMissingOpenShiftFiles(found *openShiftPaths) []string {
	var missing []string

	if !found.hasMetadata {
		missing = append(missing, "openshift/metadata.yaml")
	}
	if !found.hasChart {
		missing = append(missing, "openshift/Chart.yaml")
	}
	if !found.hasValues {
		missing = append(missing, "openshift/values.yaml")
	}
	if !found.hasSchema {
		missing = append(missing, "openshift/values.schema.json")
	}
	if !found.hasTemplFile {
		missing = append(missing, "openshift/templates/*.yaml (at least one)")
	}

	return missing
}

// helmValidateOpenShift loads the provided openshift/ file slice (already collected
// by collectOpenShiftPaths — no further archive scan) as an in-memory Helm chart and
// runs two validation stages:
//  1. chart.Validate() — verifies Chart.yaml metadata (apiVersion, name, semver version, type).
//  2. Chart version equality — chrt.Metadata.Version must equal rootVersion; using the
//     already-parsed value avoids a redundant YAML decode of Chart.yaml.
//  3. engine.Render()  — renders all templates against the chart's default values to
//     surface Go template syntax errors and undefined variable references.
//
// No filesystem writes are performed.
func helmValidateOpenShift(files []*archive.BufferedFile, rootVersion string) error {
	chrt, err := loader.LoadFiles(files)
	if err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("openshift Helm chart is invalid: %s", err),
		}
	}

	if err := chrt.Validate(); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("openshift Chart.yaml is invalid: %s", err),
		}
	}

	// chart version equality check — reuses the already-parsed chrt.Metadata.Version
	// instead of re-decoding Chart.yaml bytes a second time.
	if chrt.Metadata.Version != rootVersion {
		return &validators.ValidationError{
			Code: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf(
				"version mismatch: root metadata.yaml has %q but openshift/Chart.yaml has %q",
				rootVersion, chrt.Metadata.Version,
			),
		}
	}

	renderVals, err := chartcommonutil.ToRenderValues(chrt, nil, common.ReleaseOptions{
		Name:      "validation",
		Namespace: "default",
	}, nil)
	if err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("openshift chart values are invalid: %s", err),
		}
	}

	if _, err := engine.Render(chrt, renderVals); err != nil {
		return &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("openshift Helm templates failed to render: %s", err),
		}
	}

	return nil
}

// Ensure OpenShiftBundleValidator implements BundleValidator at compile time.
var _ BundleValidator = (*OpenShiftBundleValidator)(nil)
