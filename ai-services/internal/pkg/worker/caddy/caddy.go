// Package caddy manages the Caddy reverse-proxy pod that runs on a worker node.
// It writes the Caddyfile and deploys the Caddy pod so that the worker can
// serve proxied routes on behalf of the control plane.
//
// Pod templates are rendered via EmbedTemplateProvider using assets/worker,
// following the same pattern as the catalog deploy context.
package caddy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	ttemplate "text/template"

	"github.com/project-ai-services/ai-services/assets"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
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

	// workerApp is the app name passed to the template provider.
	// Resolves to assets/worker/<runtime>/templates/.
	workerApp = "worker"

	workerCaddyfileSubDir = "worker/caddy"
	workerCaddyfilePath   = "worker/podman/Caddyfile.tmpl"

	dirPerm  = 0o755
	filePerm = 0o644
)

// SetupOptions carries the parameters needed to deploy Caddy on a worker node.
type SetupOptions struct {
	// BaseDir is the host directory used for Caddy data / config volumes.
	// Set once at first join; ignored on subsequent runs if the pod is already running.
	BaseDir string
	// HTTPSPort is the host port Caddy binds for HTTPS traffic, e.g. "443".
	// Set once at first join; ignored on subsequent runs if the pod is already running.
	HTTPSPort string
}

// Setup writes the Caddyfile and deploys the worker Caddy pod.
//
// The Caddyfile is always (re)written so it stays in sync with the embedded
// template. The pod itself is only deployed if it is not already running —
// once up, its configuration is considered immutable.
func Setup(ctx context.Context, rt *podman.PodmanClient, opts SetupOptions) error {
	logger.InfolnCtx(ctx, "Setting up worker Caddy proxy...")

	if err := writeCaddyfile(opts.BaseDir); err != nil {
		return fmt.Errorf("worker caddy: write Caddyfile: %w", err)
	}

	if err := deployIfNotExists(ctx, rt, opts); err != nil {
		return err
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

// ─── deploy ───────────────────────────────────────────────────────────────────

// deployIfNotExists deploys the worker Caddy pod if it is not already running.
// Once deployed, the pod is never restarted by this tool — its config is
// considered immutable after first join.
// TODO: Need a way to implement certificate rotation in future.
func deployIfNotExists(ctx context.Context, rt *podman.PodmanClient, opts SetupOptions) error {
	exists, err := rt.PodExists(WorkerCaddyPodName)
	if err != nil {
		return fmt.Errorf("worker caddy: check pod existence: %w", err)
	}

	if exists {
		logger.InfofCtx(ctx, "worker caddy: %s already running — skipping deploy.\n", WorkerCaddyPodName)

		return nil
	}

	return deployPods(ctx, rt, opts)
}

// deployPods loads all pod templates from assets/worker/<runtime>/templates via
// EmbedTemplateProvider and deploys each one in the order defined by
// metadata.yaml podTemplateExecutions — matching the catalog deploy pattern.
func deployPods(ctx context.Context, rt *podman.PodmanClient, opts SetupOptions) error {
	tp := templates.NewEmbedTemplateProvider(&assets.WorkerFS, workerApp)

	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(workerApp, true, &appMetadata); err != nil {
		return fmt.Errorf("worker caddy: load metadata: %w", err)
	}

	tmpls, err := tp.LoadAllTemplates(workerApp)
	if err != nil {
		return fmt.Errorf("worker caddy: load templates: %w", err)
	}

	values, err := tp.LoadValues(workerApp, nil, map[string]string{
		"caddy.httpsPort": opts.HTTPSPort,
	})
	if err != nil {
		return fmt.Errorf("worker caddy: load values: %w", err)
	}

	params := map[string]any{
		"BaseDir": opts.BaseDir,
		"Values":  values,
	}

	for _, layer := range appMetadata.PodTemplateExecutions {
		for _, tmplName := range layer {
			if err := deployTemplate(ctx, rt, tmpls, tmplName, params); err != nil {
				return err
			}
		}
	}

	return nil
}

// deployTemplate renders a single pod template and deploys it.
// The rendered YAML is used both as the pod spec source and as the body for
// CreatePod — rendered once, parsed once.
func deployTemplate(ctx context.Context, rt *podman.PodmanClient, tmpls map[string]*ttemplate.Template, tmplName string, params map[string]any) error {
	tmpl, ok := tmpls[tmplName]
	if !ok {
		return fmt.Errorf("worker caddy: template %q not found", tmplName)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("worker caddy: render %s: %w", tmplName, err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("worker caddy: parse pod spec %s: %w", tmplName, err)
	}

	deployOpts := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(podSpec))

	logger.InfofCtx(ctx, "worker caddy: deploying %s\n", podSpec.Name)

	return clipodman.DeployPodAndReadinessCheck(ctx, rt, &podSpec, tmplName,
		bytes.NewReader(rendered.Bytes()), deployOpts)
}

// Made with Bob
