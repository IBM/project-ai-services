package podman

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	ttemplate "text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	deployutils "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/utils"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"

	k8syaml "sigs.k8s.io/yaml"
)

const (
	caddyfilePath = "worker/podman/Caddyfile.tmpl"
	kindSecret    = "Secret"
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
		return fmt.Errorf("failed to init runtime: %w", err)
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

	domainSuffix, err := utils.ComputeDomainSuffix(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath, opts.Setup.DomainName)
	if err != nil {
		return fmt.Errorf("worker join: compute domain suffix: %w", err)
	}

	if err := deployAll(ctx, rt, tp, opts, existingResource, domainSuffix); err != nil {
		return err
	}

	logger.DebugfCtx(ctx, "Using domain suffix: %s\n", domainSuffix)

	// Create Caddy context with pod name and domain suffix (NO template dependencies)
	caddyCtx := caddy.NewContext(workerconstants.WorkerCaddyPodName, domainSuffix)

	// Load SSL certificates if provided
	if err := caddyCtx.LoadSSLCertificates(ctx, opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath); err != nil {
		return err
	}

	if err := deployutils.CheckWorkerContainerLogs(ctx, rt); err != nil {
		cleanupFailedWorkerPods(ctx, rt)

		return err
	}

	logger.InfolnCtx(ctx, "Worker node setup complete.")

	return nil
}

// cleanupFailedWorkerPods deletes all worker pods after a failed join attempt.
func cleanupFailedWorkerPods(ctx context.Context, rt runtime.Runtime) {
	pods, listErr := rt.ListPods(ctx, map[string][]string{"label": {workerconstants.WorkerPodLabel}})
	if listErr != nil {
		logger.ErrorfCtx(ctx, "failed to list worker pods for cleanup: %v\n", listErr)

		return
	}

	for _, pod := range pods {
		logger.InfofCtx(ctx, "Deleting '%s' pod, as worker failed to join", pod.Name)
		if delErr := rt.DeletePod(ctx, pod.ID, utils.BoolPtr(true)); delErr != nil {
			logger.ErrorfCtx(ctx, "failed to delete worker pod %s: %v\n", pod.Name, delErr)
		}
	}
}

// IsWorkerDeployed reports whether the local worker pod and all its secrets are
// already present. It is used by JoinAsLocalWorker to skip re-registration when
// the worker was preserved by a --skip-cleanup uninstall.
func IsWorkerDeployed(ctx context.Context, rt runtime.Runtime) (bool, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.WorkerFS, "")
	deployed, _, err := CheckStatus(ctx, rt, tp)

	return deployed, err
}

// CheckStatus checks whether the worker node is already deployed by listing
// pods with the worker proxy and worker pod labels.
// Returns (true, existingResources, nil) when all worker pods are already running.
func CheckStatus(ctx context.Context, rt runtime.Runtime, tp templates.Template) (bool, []string, error) {
	labels := []string{workerconstants.WorkerProxyLabel, workerconstants.WorkerPodLabel}
	tmpls, err := tp.LoadAllTemplates(workerconstants.WorkerAppTemplate)
	if err != nil {
		return false, nil, fmt.Errorf("failed to load templates: %w", err)
	}

	secretNames, err := collectWorkerSecretNames(tmpls)
	if err != nil {
		return false, nil, fmt.Errorf("failed to collect secret names: %w", err)
	}

	existingResources, err := helpers.CheckExistingResourcesForWorker(ctx, rt, labels, secretNames)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check existing resources: %w", err)
	}

	logger.InfofCtx(ctx, "List of existing resources: %v", existingResources)

	workerResourceCount := len(tmpls)

	// Checking if 'caddy-cert-secret' optional secret is present or not
	exists, err := rt.SecretExists(ctx, workerconstants.CaddyCertSecretName)
	if err != nil {
		return false, nil, err
	}
	if exists {
		existingResources = append(existingResources, workerconstants.CaddyCertSecretName)
	} else {
		// When 'caddy-cert-secret' secret not created, decrement workerResourceCount by one,
		// as resource is created based on optional flag (--ssl-cert and --ssl-key)
		workerResourceCount--
	}

	return len(existingResources) == workerResourceCount, existingResources, nil
}

// collectWorkerSecretNames renders each worker template with empty params and
// returns the names of all resources whose Kind is "Secret".
func collectWorkerSecretNames(tmpls map[string]*ttemplate.Template) ([]string, error) {
	var secretNames []string

	for tmplName, tmpl := range tmpls {
		var rendered bytes.Buffer
		if err := tmpl.Execute(&rendered, nil); err != nil {
			// Skip templates that require params — they are not secrets.
			return nil, err
		}

		if strings.TrimSpace(rendered.String()) == "" {
			continue
		}

		var podSpec podmodels.PodSpec
		if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
			return nil, fmt.Errorf("failed to parse template %q: %w", tmplName, err)
		}

		if podSpec.Kind == kindSecret {
			secretNames = append(secretNames, podSpec.Name)
		}
	}

	return secretNames, nil
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
func deployAll(ctx context.Context, rt runtime.Runtime, tp templates.Template, opts workertypes.PodmanWorkerOptions, existingResources []string, domainSuffix string) error {
	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(workerconstants.WorkerAppTemplate, true, &appMetadata); err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	tmpls, err := tp.LoadAllTemplates(workerconstants.WorkerAppTemplate)
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	argParams, err := buildArgParams(opts)
	if err != nil {
		return err
	}

	values, err := tp.LoadValues(workerconstants.WorkerAppTemplate, nil, argParams)
	if err != nil {
		return fmt.Errorf("failed to load values: %w", err)
	}

	values["hostAliases"] = opts.Setup.HostAliases

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
		return nil, fmt.Errorf("failed to resolve podman URI: %w", err)
	}

	authFileBase64, err := utils.ReadAuthFileBase64()
	if err != nil {
		return nil, err
	}

	caddyFileContent, sslCertContent, sslKeyContent, err := readCaddyConfig(opts.Setup.SSLCertPath, opts.Setup.SSLKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Caddyfile: %w", err)
	}

	return map[string]string{
		constants.ArgParamCaddyHTTPSPort:          strconv.Itoa(opts.Setup.HTTPSPort),
		constants.ArgParamCaddyFileContent:        utils.IndentString(caddyFileContent, utils.CaddyFileIndent),
		constants.ArgParamSSLCertFileContent:      utils.IndentString(sslCertContent, utils.CertContentIndent),
		constants.ArgParamSSLKeyFileContent:       utils.IndentString(sslKeyContent, utils.CertContentIndent),
		workerconstants.ArgParamWorkerToken:       opts.Token,
		workerconstants.ArgParamWorkerGatewayAddr: opts.GatewayAddr,
		workerconstants.ArgParamWorkerPodmanURI:   strings.TrimPrefix(podmanURI, "unix://"),
		workerconstants.ArgParamWorkerAuthFile:    authFileBase64,
	}, nil
}

// renderAndDeploy renders a single pod template and deploys it.
// The rendered YAML is used both as the pod spec source and as the body for
// CreatePod — rendered once, parsed once.
func renderAndDeploy(ctx context.Context, rt runtime.Runtime, tmpls map[string]*ttemplate.Template, tmplName string, params map[string]any, existingResources []string) error {
	tmpl, ok := tmpls[tmplName]
	if !ok {
		return fmt.Errorf("template %q not found", tmplName)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to render template %s: %w", tmplName, err)
	}

	// If the rendered template is empty, skip deploying it
	if strings.TrimSpace(rendered.String()) == "" {
		logger.Infof("%s: Skipping resource deploy as it rendered empty", tmplName)

		return nil
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("failed to parse pod spec %s: %w", tmplName, err)
	}
	// Skipping deployment of existing resources
	if slices.Contains(existingResources, podSpec.Name) {
		logger.Infof("%s: Skipping resource deploy as '%s' it already exists", tmplName, podSpec.Name)

		return nil
	}

	deployOpts := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(podSpec))

	logger.InfofCtx(ctx, "Deploying %s\n", podSpec.Name)

	return clipodman.DeployPodAndReadinessCheck(ctx, rt, &podSpec, tmplName,
		bytes.NewReader(rendered.Bytes()), deployOpts)
}
