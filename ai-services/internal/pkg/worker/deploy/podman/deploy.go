// Package deploy provides worker-node setup and pod deployment helpers.
// It writes prerequisite config files (e.g. Caddyfile), checks whether worker
// components are already running, and deploys pods from the assets/worker
// template tree via EmbedTemplateProvider.
package deploy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	ttemplate "text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"

	k8syaml "sigs.k8s.io/yaml"
)

const (
	caddyfilePath = "worker/podman/Caddyfile.tmpl"
)

// Options carries the parameters needed to set up the worker node.
type Options struct {
	// BaseDir is the host directory used for worker data / config volumes
	// (Caddy config, models, etc.).
	// Set once at first join; ignored on subsequent runs if already deployed.
	BaseDir string
	// HTTPSPort is the host port Caddy binds for HTTPS traffic, e.g. "443".
	// Set once at first join; ignored on subsequent runs if already deployed.
	HTTPSPort int

	// DomainName is an optional custom domain suffix for self-signed certificates.
	// If empty, Caddy uses wildcard DNS format: <service>.<ip>.nip.io.
	DomainName string
	// SSLCertPath is the path to a user-provided SSL certificate (PEM).
	// Must be used together with SSLKeyPath.
	SSLCertPath string
	// SSLKeyPath is the path to a user-provided SSL private key (PEM).
	// Must be used together with SSLCertPath.
	SSLKeyPath string
}

// DeployWorker writes prerequisite config files, checks whether the worker proxy
// pod is already running, and deploys all worker pods defined in
// assets/worker/<runtime>/metadata.yaml podTemplateExecutions.
//
// The Caddyfile is always (re)written so it stays in sync with the embedded
// template. Pods are only deployed if not already running — once up, their
// configuration is considered immutable.
// TODO: Need a way to implement certificate rotation in future.
func DeployWorker(ctx context.Context, opts workertypes.PodmanWorkerOptions) error {
	logger.InfolnCtx(ctx, "Setting up worker node...")

	rt, err := runtime.CreateRuntime(types.RuntimeTypePodman, "")
	if err != nil {
		return fmt.Errorf("worker join: init runtime: %w", err)
	}

	tp := templates.NewEmbedTemplateProvider(&assets.WorkerFS, "")

	deployed, existingResource, err := CheckStatus(ctx, rt, tp)
	if err != nil {
		return err
	}

	if deployed {
		logger.InfolnCtx(ctx, "Worker node already set up — skipping deploy.")

		return nil
	}

	if err := deployAll(ctx, rt, tp, opts, existingResource); err != nil {
		return err
	}

	caddyCtx, err := setupCaddyContext(ctx, opts)
	// Load SSL certificates if provided
	if err := caddyCtx.LoadSSLCertificates(ctx, opts.Setup.BaseDir, opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath); err != nil {
		return err
	}

	logger.InfolnCtx(ctx, "Worker node setup complete.")

	return nil
}

// CheckStatus checks whether the worker node is already deployed by listing
// pods with the worker proxy and worker pod labels.
// Returns (true, existingResources, nil) when all worker pods are already running.
func CheckStatus(ctx context.Context, rt runtime.Runtime, tp templates.Template) (bool, []string, error) {
	labels := []string{workerconstants.WorkerProxyLabel, workerconstants.WorkerPodLabel}

	var existingResources []string
	for _, label := range labels {
		pods, err := rt.ListPods(ctx, map[string][]string{"label": {label}})
		if err != nil {
			return false, nil, fmt.Errorf("worker setup: list pods: %w", err)
		}

		for _, p := range pods {
			existingResources = append(existingResources, p.Name)
		}
	}

	logger.InfofCtx(ctx, "List of existing resources: %v", existingResources)

	tmpls, err := tp.LoadAllTemplates(workerconstants.WorkerAppTemplate)
	if err != nil {
		return false, nil, fmt.Errorf("worker setup: load templates: %w", err)
	}

	return len(existingResources) == len(tmpls), existingResources, nil
}

// ─── internal ────────────────────────────────────────────────────────────────

func readCaddyConfig(sslCertPath, sslKeyPath string) (string, string, string, error) {
	raw, err := assets.WorkerFS.ReadFile(caddyfilePath)
	if err != nil {
		return "", "", "", fmt.Errorf("read Caddyfile: %w", err)
	}

	var sslCertContent, sslKeyContent string
	if sslCertPath != "" && sslKeyPath != "" {
		certbyte, keyBytes, _, err := utils.ReadAndParseCertificates(sslCertPath, sslKeyPath)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to load ssl certs: %w", err)
		}
		sslCertContent = string(certbyte)
		sslKeyContent = string(keyBytes)
	}

	return string(raw), sslCertContent, sslKeyContent, nil
}

// deployAll loads all pod templates from assets/worker/<runtime>/templates and
// deploys each one in the order defined by metadata.yaml podTemplateExecutions.
func deployAll(ctx context.Context, rt runtime.Runtime, tp templates.Template, opts workertypes.PodmanWorkerOptions, existingResources []string) error {
	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(workerconstants.WorkerAppTemplate, true, &appMetadata); err != nil {
		return fmt.Errorf("worker setup: load metadata: %w", err)
	}

	tmpls, err := tp.LoadAllTemplates(workerconstants.WorkerAppTemplate)
	if err != nil {
		return fmt.Errorf("worker setup: load templates: %w", err)
	}

	domainSuffix, err := utils.ComputeDomainSuffix(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath, opts.Setup.DomainName)
	if err != nil {
		return fmt.Errorf("worker setup: compute domain suffix: %w", err)
	}

	argParams, err := buildArgParams(opts)
	if err != nil {
		return err
	}

	values, err := tp.LoadValues(workerconstants.WorkerAppTemplate, nil, argParams)
	if err != nil {
		return fmt.Errorf("worker setup: load values: %w", err)
	}

	params := map[string]any{
		"BaseDir":         opts.Setup.BaseDir,
		"AppName":         workerconstants.WorkerAppName,
		"AppTemplateName": workerconstants.WorkerAppTemplate,
		"Version":         appMetadata.Version,
		"CaddyAdminURL":   fmt.Sprintf("http://%s:2019", workerconstants.WorkerCaddyPodName),
		"DomainSuffix":    domainSuffix,
		"Values":          values,
	}

	for _, layer := range appMetadata.PodTemplateExecutions {
		for _, tmplName := range layer {
			if err := renderAndDeploy(ctx, rt, tmpls, tmplName, params, existingResources); err != nil {
				return err
			}
		}
	}

	return nil
}

// buildArgParams resolves host-specific runtime values (podman socket, auth
// file) and assembles the full map of template arg overrides for deployAll.
func buildArgParams(opts workertypes.PodmanWorkerOptions) (map[string]string, error) {
	// Resolve the actual podman socket path from the host environment so the
	// worker container gets the correct CONTAINER_HOST and volume mount.
	podmanURI, err := utils.ResolvePodmanURI()
	if err != nil {
		return nil, fmt.Errorf("worker setup: resolve podman URI: %w", err)
	}

	authFileBase64, err := readAuthFileBase64()
	if err != nil {
		return nil, err
	}

	caddyFileContent, sslCertContent, sslKeyContent, err := readCaddyConfig(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath)
	if err != nil {
		return nil, fmt.Errorf("worker setup: read Caddyfile: %w", err)
	}

	return map[string]string{
		workerconstants.ArgParamCaddyHTTPSPort:     strconv.Itoa(opts.Setup.HTTPSPort),
		workerconstants.ArgParamCaddyFileContent:   utils.IndentString(caddyFileContent, utils.CaddyFileIndent),
		workerconstants.ArgsParamCaddyCertContent:  utils.IndentString(sslCertContent, utils.CertContentIndent),
		workerconstants.ArgParamCaddySslKeyContent: utils.IndentString(sslKeyContent, utils.CertContentIndent),
		workerconstants.ArgParamWorkerToken:        opts.Token,
		workerconstants.ArgParamWorkerGatewayAddr:  opts.GatewayAddr,
		workerconstants.ArgParamWorkerPodmanURI:    strings.TrimPrefix(podmanURI, "unix://"),
		workerconstants.ArgParamWorkerAuthFile:     authFileBase64,
	}, nil
}

// readAuthFileBase64 reads the podman auth file and returns its contents
// base64-encoded. If the file does not exist, it returns an encoded empty
// JSON object and logs a warning.
func readAuthFileBase64() (string, error) {
	authFilePath, err := utils.GetAuthFilePath()
	if err != nil {
		return "", fmt.Errorf("worker setup: resolve auth file path: %w", err)
	}

	content, err := os.ReadFile(authFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warningln("Podman auth file not found. Image pulls may fail if the registry requires authentication.")
			// TODO: worker join- > worker --reset-podman-auth when implemented
			logger.Warningln("Run 'podman login' then re-run 'worker join' to update credentials.")
			content = []byte("{}")
		} else {
			return "", fmt.Errorf("worker setup: read auth file %s: %w", authFilePath, err)
		}
	}

	return base64.StdEncoding.EncodeToString(content), nil
}

// renderAndDeploy renders a single pod template and deploys it.
// The rendered YAML is used both as the pod spec source and as the body for
// CreatePod — rendered once, parsed once.
func renderAndDeploy(ctx context.Context, rt runtime.Runtime, tmpls map[string]*ttemplate.Template, tmplName string, params map[string]any, existingResources []string) error {
	tmpl, ok := tmpls[tmplName]
	if !ok {
		return fmt.Errorf("worker setup: template %q not found", tmplName)
	}

	fmt.Println(params)
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("worker setup: render %s: %w", tmplName, err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("worker setup: parse pod spec %s: %w", tmplName, err)
	}
	// Skipping deployment of existing resources
	if slices.Contains(existingResources, podSpec.Name) {
		logger.Infof("%s: Skipping resource deploy as '%s' it already exists", tmplName, podSpec.Name)

		return nil
	}

	deployOpts := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(podSpec))

	logger.InfofCtx(ctx, "worker setup: deploying %s\n", podSpec.Name)

	return clipodman.DeployPodAndReadinessCheck(ctx, rt, &podSpec, tmplName,
		bytes.NewReader(rendered.Bytes()), deployOpts)
}

// setupCaddyContext sets up the Caddy context with domain configuration and Caddyfile generation.
// This function:
// 1. Computes domain configuration (cert domain extraction + domain suffix resolution)
// 2. Creates Caddy context with pod name and domain suffix.
func setupCaddyContext(ctx context.Context, opts workertypes.PodmanWorkerOptions) (*caddy.Context, error) {
	// Compute domain configuration (cert domain extraction + domain suffix resolution)
	domainSuffix, err := utils.ComputeDomainSuffix(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath, opts.Setup.DomainName)
	if err != nil {
		return nil, err
	}

	logger.DebugfCtx(ctx, "Using domain suffix: %s\n", domainSuffix)

	// Create Caddy context with pod name and domain suffix (NO template dependencies)
	caddyCtx := caddy.NewContext(workerconstants.WorkerCaddyPodName, domainSuffix)

	return caddyCtx, nil
}
