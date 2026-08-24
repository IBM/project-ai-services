// Package deploy provides worker-node setup and pod deployment helpers.
// It writes prerequisite config files (e.g. Caddyfile), checks whether worker
// components are already running, and deploys pods from the assets/worker
// template tree via EmbedTemplateProvider.
package deploy

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
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"

	k8syaml "sigs.k8s.io/yaml"
)

const (
	// workerApp is the app name passed to the template provider.
	// Resolves to assets/worker/<runtime>/templates/.
	workerApp = "worker"

	// workerProxyLabel is the label used to check whether the worker proxy
	// pod is already running — more resilient than checking by name.
	workerProxyLabel = "ai-services.io/component=worker-proxy"

	caddyfileSubDir = "worker/caddy"
	caddyfilePath   = "worker/podman/Caddyfile.tmpl"

	dirPerm  = 0o750
	filePerm = 0o644
)

// Options carries the parameters needed to set up the worker node.
type Options struct {
	// BaseDir is the host directory used for worker data / config volumes
	// (Caddy config, models, etc.).
	// Set once at first join; ignored on subsequent runs if already deployed.
	BaseDir string
	// HTTPSPort is the host port Caddy binds for HTTPS traffic, e.g. "443".
	// Set once at first join; ignored on subsequent runs if already deployed.
	HTTPSPort string
}

// Setup writes prerequisite config files, checks whether the worker proxy
// pod is already running, and deploys all worker pods defined in
// assets/worker/<runtime>/metadata.yaml podTemplateExecutions.
//
// The Caddyfile is always (re)written so it stays in sync with the embedded
// template. Pods are only deployed if not already running — once up, their
// configuration is considered immutable.
// TODO: Need a way to implement certificate rotation in future.
func Setup(ctx context.Context, rt runtime.Runtime, opts Options) error {
	logger.InfolnCtx(ctx, "Setting up worker node...")

	pods, err := rt.ListPods(map[string][]string{
		"label": {workerProxyLabel},
	})
	if err != nil {
		return fmt.Errorf("worker setup: list pods: %w", err)
	}

	if len(pods) > 0 {
		logger.InfolnCtx(ctx, "Worker node already set up — skipping deploy.")

		return nil
	}

	if err := writeCaddyfile(opts.BaseDir); err != nil {
		return fmt.Errorf("worker setup: write Caddyfile: %w", err)
	}

	if err := deployAll(ctx, rt, opts); err != nil {
		return err
	}

	logger.InfolnCtx(ctx, "Worker node setup complete.")

	return nil
}

// ─── internal ────────────────────────────────────────────────────────────────

// writeCaddyfile writes the static worker Caddyfile to
// <baseDir>/worker/caddy/Caddyfile. The Caddyfile has no template variables —
// it is written verbatim. Caddy must find it at container start.
func writeCaddyfile(baseDir string) error {
	raw, err := assets.WorkerFS.ReadFile(caddyfilePath)
	if err != nil {
		return fmt.Errorf("read Caddyfile: %w", err)
	}

	dir := filepath.Join(baseDir, caddyfileSubDir)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	dst := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(dst, raw, filePerm); err != nil {
		return fmt.Errorf("write Caddyfile to %s: %w", dst, err)
	}

	logger.Infof("worker setup: Caddyfile written to %s\n", dst)

	return nil
}

// deployAll loads all pod templates from assets/worker/<runtime>/templates and
// deploys each one in the order defined by metadata.yaml podTemplateExecutions.
func deployAll(ctx context.Context, rt runtime.Runtime, opts Options) error {
	tp := templates.NewEmbedTemplateProvider(&assets.WorkerFS, workerApp)

	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(workerApp, true, &appMetadata); err != nil {
		return fmt.Errorf("worker setup: load metadata: %w", err)
	}

	tmpls, err := tp.LoadAllTemplates(workerApp)
	if err != nil {
		return fmt.Errorf("worker setup: load templates: %w", err)
	}

	values, err := tp.LoadValues(workerApp, nil, map[string]string{
		"caddy.httpsPort": opts.HTTPSPort,
	})
	if err != nil {
		return fmt.Errorf("worker setup: load values: %w", err)
	}

	params := map[string]any{
		"BaseDir": opts.BaseDir,
		"Values":  values,
	}

	for _, layer := range appMetadata.PodTemplateExecutions {
		for _, tmplName := range layer {
			if err := renderAndDeploy(ctx, rt, tmpls, tmplName, params); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderAndDeploy renders a single pod template and deploys it.
// The rendered YAML is used both as the pod spec source and as the body for
// CreatePod — rendered once, parsed once.
func renderAndDeploy(ctx context.Context, rt runtime.Runtime, tmpls map[string]*ttemplate.Template, tmplName string, params map[string]any) error {
	tmpl, ok := tmpls[tmplName]
	if !ok {
		return fmt.Errorf("worker setup: template %q not found", tmplName)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("worker setup: render %s: %w", tmplName, err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("worker setup: parse pod spec %s: %w", tmplName, err)
	}

	deployOpts := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(podSpec))

	logger.InfofCtx(ctx, "worker setup: deploying %s\n", podSpec.Name)

	return clipodman.DeployPodAndReadinessCheck(ctx, rt, &podSpec, tmplName,
		bytes.NewReader(rendered.Bytes()), deployOpts)
}
