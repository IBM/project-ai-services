// Package caddy manages the Caddy reverse-proxy pod that runs on a worker node.
// It writes the Caddyfile and deploys the Caddy pod so that the worker can
// serve proxied routes on behalf of the control plane.
//
// This package intentionally mirrors the pattern used in internal/pkg/agent/configure
// from the remote_2_3 branch: values.yaml is parsed into a typed struct, the pod
// template is rendered directly from raw bytes, and the pod is always removed and
// redeployed to ensure correct port bindings.
package caddy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"

	k8syaml "sigs.k8s.io/yaml"
)

const (
	// WorkerCaddyPodName is the fixed name of the worker Caddy pod.
	// Exported so that the join flow can reference it after setup.
	WorkerCaddyPodName = "ai-services--worker-caddy"

	workerCaddyfileSubDir = "worker/caddy"
	workerCaddyTmplPath   = "worker/podman/templates/caddy.yaml.tmpl"
	workerCaddyfilePath   = "worker/podman/Caddyfile.tmpl"
	workerValuesPath      = "worker/podman/values.yaml"

	dirPerm  = 0o755
	filePerm = 0o644
)

// SetupOptions carries the parameters needed to deploy Caddy on a worker node.
type SetupOptions struct {
	// BaseDir is the host directory used for Caddy data / config volumes.
	BaseDir string
	// HTTPSPort is the host port Caddy binds for HTTPS traffic, e.g. "443".
	// Set by the CLI flag default — never empty when called from join.
	HTTPSPort string
}

// Setup writes the Caddyfile and deploys the worker Caddy pod.
//
// If an existing pod is running it is removed first. Routes are preserved
// across restarts because the pod starts with --resume, which reloads
// Caddy's autosaved config from /config/caddy/autosave.json (persisted to
// <baseDir>/worker/caddy-config on the host).
func Setup(ctx context.Context, rt *podman.PodmanClient, opts SetupOptions) error {
	vals, err := readValues()
	if err != nil {
		return fmt.Errorf("worker caddy: read values: %w", err)
	}

	if vals.Caddy.Image == "" {
		return fmt.Errorf("worker caddy: caddy.image not set in values.yaml")
	}

	// CLI-supplied port overrides the (empty) value from values.yaml.
	vals.Caddy.HTTPSPort = opts.HTTPSPort

	logger.InfolnCtx(ctx, "Setting up worker Caddy proxy...")

	if err := writeCaddyfile(opts.BaseDir); err != nil {
		return fmt.Errorf("worker caddy: write Caddyfile: %w", err)
	}

	// Remove any existing pod so port bindings are always fresh.
	// Routes survive because --resume reloads the autosave on startup.
	exists, err := rt.PodExists(WorkerCaddyPodName)
	if err != nil {
		return fmt.Errorf("worker caddy: check pod existence: %w", err)
	}

	if exists {
		logger.InfofCtx(ctx, "worker caddy: %s exists — removing before redeploy\n", WorkerCaddyPodName)
		force := true
		if err := rt.DeletePod(WorkerCaddyPodName, &force); err != nil {
			return fmt.Errorf("worker caddy: remove existing pod: %w", err)
		}
	}

	if err := deployPod(ctx, rt, opts.BaseDir, vals); err != nil {
		return fmt.Errorf("worker caddy: deploy pod: %w", err)
	}

	logger.InfolnCtx(ctx, "Worker Caddy proxy is ready.")

	return nil
}

// ─── internal ────────────────────────────────────────────────────────────────

// writeCaddyfile writes the static worker Caddyfile to
// <baseDir>/worker/caddy/Caddyfile. The template has no variables — it is
// written verbatim.
func writeCaddyfile(baseDir string) error {
	raw, err := assets.WorkerFS.ReadFile(workerCaddyfilePath)
	if err != nil {
		return fmt.Errorf("read Caddyfile template: %w", err)
	}

	caddyDir := filepath.Join(baseDir, workerCaddyfileSubDir)
	if err := os.MkdirAll(caddyDir, dirPerm); err != nil {
		return fmt.Errorf("create caddy dir %s: %w", caddyDir, err)
	}

	dst := filepath.Join(caddyDir, "Caddyfile")
	if err := os.WriteFile(dst, raw, filePerm); err != nil {
		return fmt.Errorf("write Caddyfile to %s: %w", dst, err)
	}

	logger.Infof("worker caddy: Caddyfile written to %s\n", dst)

	return nil
}

// deployPod renders the pod template from raw bytes and calls
// DeployPodAndReadinessCheck, mirroring the agent configure pattern.
func deployPod(ctx context.Context, rt *podman.PodmanClient, baseDir string, vals *workerCaddyValues) error {
	raw, err := assets.WorkerFS.ReadFile(workerCaddyTmplPath)
	if err != nil {
		return fmt.Errorf("read pod template: %w", err)
	}

	tmpl, err := template.New("caddy.yaml.tmpl").Parse(string(raw))
	if err != nil {
		return fmt.Errorf("parse pod template: %w", err)
	}

	params := map[string]any{
		"BaseDir": baseDir,
		"Values": map[string]any{
			"caddy": map[string]any{
				"image":     vals.Caddy.Image,
				"adminPort": vals.Caddy.AdminPort,
				"httpsPort": vals.Caddy.HTTPSPort,
			},
		},
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("render pod template: %w", err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("parse rendered pod YAML: %w", err)
	}

	deployOpts := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(podSpec))

	logger.InfofCtx(ctx, "worker caddy: deploying %s\n", WorkerCaddyPodName)

	return clipodman.DeployPodAndReadinessCheck(
		ctx, rt, &podSpec, "caddy.yaml.tmpl",
		bytes.NewReader(rendered.Bytes()), deployOpts,
	)
}

// ─── values ───────────────────────────────────────────────────────────────────

// workerCaddyValues mirrors the structure of assets/worker/podman/values.yaml.
type workerCaddyValues struct {
	Caddy struct {
		Image     string `yaml:"image"`
		AdminPort string `yaml:"adminPort"`
		HTTPSPort string `yaml:"httpsPort"`
	} `yaml:"caddy"`
}

// readValues parses assets/worker/podman/values.yaml.
func readValues() (*workerCaddyValues, error) {
	raw, err := assets.WorkerFS.ReadFile(workerValuesPath)
	if err != nil {
		return nil, fmt.Errorf("read values.yaml: %w", err)
	}

	var vals workerCaddyValues
	if err := k8syaml.Unmarshal(raw, &vals); err != nil {
		return nil, fmt.Errorf("parse values.yaml: %w", err)
	}

	return &vals, nil
}

// Made with Bob
