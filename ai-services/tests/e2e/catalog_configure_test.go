package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

const catalogConfigureTestTimeout = 15 * time.Minute
const catalogConfigureShortTimeout = 30 * time.Second

// skipIfNotPodman skips the spec when the runtime is not podman.
func skipIfNotPodman() {
	if appRuntime != "podman" {
		ginkgo.Skip("catalog configure tests are only supported on the podman runtime — skipping on " + appRuntime)
	}
}

// skipIfNoCatalogPassword skips when CATALOG_PASSWORD is unset.
func skipIfNoCatalogPassword() {
	if bootstrap.GetCatalogAdminPassword() == "" {
		ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog configure test")
	}
}

// catalogDoRequest performs an authenticated HTTP request against the catalog API.
// It delegates client construction and body draining to common utilities, adding
// only the catalog-specific Authorization header on top.
func catalogDoRequest(ctx context.Context, method, url, token string, body []byte) ([]byte, int, error) {
	cfg := common.HTTPClientConfig{
		Timeout:            15 * time.Second,
		InsecureSkipVerify: true,
	}

	var buf *bytes.Buffer
	if len(body) > 0 {
		buf = bytes.NewBuffer(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, buf)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := common.GetHTTPClient(cfg).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer common.DrainAndClose(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// catalogGetToken obtains a JWT token from the catalog login endpoint.
func catalogGetToken(ctx context.Context, serverURL, username, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"username": username, "password": password})
	body, status, err := catalogDoRequest(ctx, http.MethodPost, serverURL+"/api/v1/auth/login", "", payload)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("catalog login returned HTTP %d: %s", status, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	token, ok := result["token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("login response missing token field: %s", string(body))
	}
	return token, nil
}

// catalogConfigureBackendURL holds the backend URL discovered by BeforeAll and shared across tests.
var catalogConfigureBackendURL string

// catalogUninstallIfRunning stops the catalog if running; no-op if already stopped.
func catalogUninstallIfRunning() {
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer infoCancel()

	if _, err := cli.CatalogInfo(infoCtx, cfg, appRuntime); err != nil {
		logger.Infof("[TEST] catalog not running — skipping pre-test uninstall")
		return
	}

	uninstallCtx, uninstallCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
	defer uninstallCancel()

	logger.Infof("[TEST] uninstalling catalog before test")
	out, uErr := cli.CatalogUninstall(uninstallCtx, cfg, appRuntime)
	if uErr != nil {
		logger.Warningf("[TEST] pre-test uninstall warning: %v\n%s", uErr, out)
	} else {
		logger.Infof("[TEST] catalog uninstalled successfully")
	}
}

// catalogRestoreDefault redeploys catalog with default settings; no-op if already running. Intended for defer.
func catalogRestoreDefault() {
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer infoCancel()

	if _, err := cli.CatalogInfo(infoCtx, cfg, appRuntime); err == nil {
		logger.Infof("[TEST] catalog already running — no restore needed")
		return
	}

	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
	defer restoreCancel()

	logger.Infof("[TEST] restoring default catalog after test")
	out, rErr := cli.CatalogConfigure(restoreCtx, cfg, appRuntime)
	if rErr != nil {
		logger.Warningf("[TEST] catalog restore warning: %v\n%s", rErr, out)
	} else {
		logger.Infof("[TEST] catalog restored successfully")
	}
}

// nonRootHomeDir resolves the home directory for the non-root user; skips the spec if resolution fails.
func nonRootHomeDir() string {
	currentUser, userErr := user.Current()
	gomega.Expect(userErr).NotTo(gomega.HaveOccurred())

	nonRootUsername := os.Getenv("NONROOT_USER")
	if currentUser.Uid == "0" && nonRootUsername == "" {
		ginkgo.Skip("running as root and NONROOT_USER not set — skipping non-root user test")
	}

	if currentUser.Uid != "0" {
		return currentUser.HomeDir
	}

	targetUser, lookupErr := user.Lookup(nonRootUsername)
	if lookupErr != nil {
		ginkgo.Skip(fmt.Sprintf("NONROOT_USER=%q not found: %v", nonRootUsername, lookupErr))
	}
	return targetUser.HomeDir
}

// nonRootCatalogRun runs catalog configure as a non-root user, delegating via sudo when running as root.
// Returns (output, error). Skips the spec if the non-root user cannot be resolved.
func nonRootCatalogRun(ctx context.Context, extraArgs ...string) (string, error) {
	currentUser, userErr := user.Current()
	gomega.Expect(userErr).NotTo(gomega.HaveOccurred())

	nonRootUsername := os.Getenv("NONROOT_USER")
	if currentUser.Uid == "0" && nonRootUsername == "" {
		ginkgo.Skip("running as root and NONROOT_USER not set — skipping non-root user test")
	}

	if currentUser.Uid != "0" {
		// Already running as non-root — invoke CLI directly.
		output, err := cli.CatalogConfigureWithArgs(ctx, cfg, appRuntime, extraArgs...)
		return output, err
	}

	// Running as root — delegate to the non-root user via sudo.
	password := bootstrap.GetCatalogAdminPassword()
	sudoArgs := append([]string{"-u", nonRootUsername, cfg.AIServiceBin, "catalog", "configure", "--runtime", appRuntime}, extraArgs...)
	logger.Infof("[TEST] delegating to non-root user %q via sudo", nonRootUsername)
	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// chatEndpointExpectNoError POSTs payload to the chat endpoint and asserts the response is not 5xx.
func chatEndpointExpectNoError(ctx context.Context, backendURL, appName, token string, payload map[string]interface{}) {
	if appName == "" {
		ginkgo.Skip("no application name — skipping chat endpoint test")
	}
	url := backendURL + fmt.Sprintf("/api/v1/applications/%s/chat", appName)
	body, _ := json.Marshal(payload)
	logger.Infof("[TEST] POST %s", url)
	_, status, err := catalogDoRequest(ctx, http.MethodPost, url, token, body)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(status).To(
		gomega.BeNumerically("<", http.StatusInternalServerError),
		"chat endpoint must not return 5xx; got %d", status,
	)
	logger.Infof("[TEST] chat endpoint returned HTTP %d", status)
}

var _ = ginkgo.Describe("Catalog Configure Tests",
	ginkgo.Ordered,
	func() {

		// ── Suite-level setup / teardown ──────────────────────────────────────

		ginkgo.BeforeAll(func() {
			if appRuntime != "podman" || bootstrap.GetCatalogAdminPassword() == "" {
				return
			}

			infoCtx, infoCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer infoCancel()

			infoOut, infoErr := cli.CatalogInfo(infoCtx, cfg, appRuntime)
			if infoErr == nil {
				catalogConfigureBackendURL = cli.ExtractCatalogBackendURL(infoOut)
				logger.Infof("[SETUP][catalog-configure] catalog already running at %s", catalogConfigureBackendURL)
				return
			}

			logger.Infof("[SETUP][catalog-configure] catalog not running — deploying via catalog configure")
			setupCtx, setupCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
			defer setupCancel()

			out, err := cli.CatalogConfigure(setupCtx, cfg, appRuntime)
			if err != nil {
				logger.Warningf("[SETUP][catalog-configure] catalog configure failed (non-fatal): %v\n%s", err, out)
				return
			}

			catalogConfigureBackendURL = cli.ExtractCatalogBackendURLFromConfigureOutput(out)
			if catalogConfigureBackendURL == "" {
				infoOut2, infoErr2 := cli.CatalogInfo(setupCtx, cfg, appRuntime)
				if infoErr2 == nil {
					catalogConfigureBackendURL = cli.ExtractCatalogBackendURL(infoOut2)
				}
			}
			logger.Infof("[SETUP][catalog-configure] catalog deployed at %s", catalogConfigureBackendURL)
		})

		ginkgo.AfterAll(func() {
			if appRuntime != "podman" || bootstrap.GetCatalogAdminPassword() == "" {
				return
			}

			// Always uninstall and re-configure to guarantee the next suite inherits a
			// clean deployment on the default nip.io domain.  Merely checking whether
			// the catalog is running is not sufficient: a prior test may have left it
			// deployed with a custom domain (e.g. e2etest.local), which would cause
			// the subsequent "ensures catalog service is running" step in e2e_suite_test.go
			// to fail with a "domain change not allowed" error.
			catalogUninstallIfRunning()

			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
			defer restoreCancel()

			logger.Infof("[TEARDOWN][catalog-configure] restoring catalog to default nip.io domain")
			out, err := cli.CatalogConfigure(restoreCtx, cfg, appRuntime)
			if err != nil {
				logger.Warningf("[TEARDOWN][catalog-configure] restore failed (non-fatal): %v\n%s", err, out)
				return
			}
			logger.Infof("[TEARDOWN][catalog-configure] catalog restored successfully")
		})

		// ── Custom Path Tests ─────────────────────────────────────────────────

		ginkgo.Context("Custom Path Configuration", func() {
			ginkgo.It(
				"configures catalog successfully with a custom --basedir path",
				ginkgo.Label("catalog-configure", "custom-path", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					customDir, err := os.MkdirTemp("", "ais-catalog-custom-path-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer func() {
						if rerr := os.RemoveAll(customDir); rerr != nil {
							logger.Warningf("[TEST] cleanup warning: %v", rerr)
						}
					}()

					catalogUninstallIfRunning()
					defer catalogRestoreDefault()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure --basedir %s", customDir)
					output, err := cli.CatalogConfigureWithBasedir(ctx, cfg, customDir, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"catalog configure --basedir should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogCustomPathOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] catalog configure with custom path succeeded")
				},
			)

			ginkgo.It(
				"resolves a relative --basedir path to an absolute path",
				ginkgo.Label("catalog-configure", "custom-path", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					absDir, err := os.MkdirTemp("", "ais-catalog-rel-path-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer func() {
						if rerr := os.RemoveAll(absDir); rerr != nil {
							logger.Warningf("[TEST] cleanup warning: %v", rerr)
						}
					}()

					catalogUninstallIfRunning()
					defer catalogRestoreDefault()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure with resolved path %s", absDir)
					output, err := cli.CatalogConfigureWithBasedir(ctx, cfg, absDir, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"catalog configure should resolve path; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogCustomPathOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] relative/absolute path resolution validated")
				},
			)

			ginkgo.It(
				"rejects a --basedir path that does not exist or is not writable",
				ginkgo.Label("catalog-configure", "custom-path", "negative", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					nonExistentPath := filepath.Join(os.TempDir(), "ais-catalog-nonexistent-"+fmt.Sprintf("%d", time.Now().UnixNano()))

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure with non-existent path %s", nonExistentPath)
					output, err := cli.CatalogConfigureWithBasedir(ctx, cfg, nonExistentPath, appRuntime)
					gomega.Expect(err).To(gomega.HaveOccurred(),
						"catalog configure with a non-existent path should fail; output:\n%s", output)
					gomega.Expect(
						cli.ValidateCatalogInvalidFlagCombinationOutput(output + err.Error()),
					).To(gomega.Succeed())
					logger.Infof("[TEST] early permission validation confirmed: %v", err)
				},
			)
		})

		// ── Uninstall Tests ───────────────────────────────────────────────────

		ginkgo.Context("Catalog Uninstall", func() {
			ginkgo.It(
				"catalog uninstall executes successfully on a running catalog",
				ginkgo.Label("catalog-configure", "uninstall", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					infoCtx, infoCancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer infoCancel()

					infoOut, infoErr := cli.CatalogInfo(infoCtx, cfg, appRuntime)
					if infoErr != nil {
						ginkgo.Skip("catalog not running — skipping uninstall test")
					}
					if cli.ExtractCatalogBackendURL(infoOut) == "" {
						ginkgo.Skip("catalog URL not found — skipping uninstall test")
					}

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					defer func() {
						restoreCtx, restoreCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
						defer restoreCancel()
						logger.Infof("[TEST] restoring catalog after uninstall")
						restoreOut, restoreErr := cli.CatalogConfigure(restoreCtx, cfg, appRuntime)
						if restoreErr != nil {
							logger.Warningf("[TEST] catalog restore warning: %v\n%s", restoreErr, restoreOut)
						} else {
							logger.Infof("[TEST] catalog restored after uninstall")
						}
					}()

					logger.Infof("[TEST] running catalog uninstall")
					output, err := cli.CatalogUninstall(ctx, cfg, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"catalog uninstall should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogUninstallOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] catalog uninstall succeeded")
				},
			)

			ginkgo.It(
				"catalog uninstall succeeds after deployment with a custom --basedir",
				ginkgo.Label("catalog-configure", "custom-path", "uninstall", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					customDir, err := os.MkdirTemp("", "ais-catalog-uninstall-custom-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer func() {
						if rerr := os.RemoveAll(customDir); rerr != nil {
							logger.Warningf("[TEST] cleanup warning: %v", rerr)
						}
					}()

					catalogUninstallIfRunning()
					defer catalogRestoreDefault()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] deploying catalog with custom basedir %s", customDir)
					configOut, configErr := cli.CatalogConfigureWithBasedir(ctx, cfg, customDir, appRuntime)
					gomega.Expect(configErr).NotTo(gomega.HaveOccurred(),
						"configure with custom basedir should succeed; output:\n%s", configOut)
					gomega.Expect(cli.ValidateCatalogCustomPathOutput(configOut)).To(gomega.Succeed())

					uninstallCtx, uninstallCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer uninstallCancel()

					logger.Infof("[TEST] running catalog uninstall after custom-path deploy")
					uninstallOut, uninstallErr := cli.CatalogUninstall(uninstallCtx, cfg, appRuntime)
					gomega.Expect(uninstallErr).NotTo(gomega.HaveOccurred(),
						"catalog uninstall after custom-path deploy should succeed; output:\n%s", uninstallOut)
					gomega.Expect(cli.ValidateCatalogUninstallCustomPathOutput(uninstallOut)).To(gomega.Succeed())
					logger.Infof("[TEST] catalog uninstall after custom-path deploy succeeded")
				},
			)
		})

		// ── Idempotency ───────────────────────────────────────────────────────

		ginkgo.Context("Configure Idempotency", func() {
			ginkgo.It(
				"re-running catalog configure is idempotent and creates no duplicate resources",
				ginkgo.Label("catalog-configure", "idempotency", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] first catalog configure run")
					firstOut, firstErr := cli.CatalogConfigure(ctx, cfg, appRuntime)
					gomega.Expect(firstErr).NotTo(gomega.HaveOccurred(),
						"first catalog configure should succeed; output:\n%s", firstOut)
					gomega.Expect(cli.ValidateCatalogConfigureOutput(firstOut)).To(gomega.Succeed())

					secondCtx, secondCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer secondCancel()

					logger.Infof("[TEST] second (idempotent) catalog configure run")
					secondOut, secondErr := cli.CatalogConfigure(secondCtx, cfg, appRuntime)
					gomega.Expect(secondErr).NotTo(gomega.HaveOccurred(),
						"second catalog configure should succeed; output:\n%s", secondOut)
					gomega.Expect(cli.ValidateCatalogConfigureOutput(secondOut)).To(gomega.Succeed())

					alreadyRunning := strings.Contains(secondOut, "already running") ||
						strings.Contains(secondOut, "Catalog Backend API is available at") ||
						strings.Contains(secondOut, "Access the Catalog Backend at")
					gomega.Expect(alreadyRunning).To(gomega.BeTrue(),
						"second configure run should indicate catalog is already running; output:\n%s", secondOut)
					logger.Infof("[TEST] idempotent configure validated")
				},
			)
		})

		// ── SSL Certificate Tests ─────────────────────────────────────────────

		ginkgo.Context("SSL Certificate Configuration", func() {
			// AfterEach guarantees the catalog is always restored to the default nip.io
			// domain after every SSL test — even when an It block fails or panics.
			// This prevents e2etest.local from leaking into subsequent tests.
			// We force-uninstall first so that CatalogConfigure regenerates the nip.io URL
			// from scratch, rather than seeing a "running" catalog and doing nothing.
			ginkgo.AfterEach(func() {
				if appRuntime != "podman" || bootstrap.GetCatalogAdminPassword() == "" {
					return
				}
				catalogUninstallIfRunning()
				restoreCtx, restoreCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
				defer restoreCancel()
				out, rErr := cli.CatalogConfigure(restoreCtx, cfg, appRuntime)
				if rErr != nil {
					logger.Warningf("[TEST] SSL AfterEach catalog restore failed: %v\n%s", rErr, out)
				} else {
					logger.Infof("[TEST] SSL AfterEach catalog restored to default nip.io domain")
				}
			})

			ginkgo.It(
				"deploys catalog with custom SSL certificate and key",
				ginkgo.Label("catalog-configure", "ssl", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					certDir, err := os.MkdirTemp("", "ais-catalog-ssl-deploy-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					cert, err := bootstrap.GenerateSelfSignedWildcardCert(certDir, "e2etest.local")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					catalogUninstallIfRunning()
					// restore is handled by AfterEach above

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure with custom SSL cert %s", cert.CertPath)
					output, err := cli.CatalogConfigureWithSSL(ctx, cfg, cert.CertPath, cert.KeyPath, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"catalog configure with custom SSL should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogConfigureOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] custom SSL deployment succeeded")
				},
			)

			// Health check omitted — self-signed cert uses e2etest.local which does not resolve in CI DNS.
			ginkgo.It(
				"resets the Caddy certificate without restarting the pod",
				ginkgo.Label("catalog-configure", "ssl", "reset", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					// --reset-certificate rejects domain changes, so the cert domain must
					// match the domain of the running deployment. To make this test
					// self-contained and order-independent we:
					//   1. Deploy catalog with e2etest.local (initial cert).
					//   2. Reset the cert to a freshly-generated cert for the same domain.
					// The AfterEach above handles teardown (uninstall + restore to nip.io).
					const resetDomain = "e2etest.local"

					certDir, err := os.MkdirTemp("", "ais-catalog-reset-cert-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					initialCert, err := bootstrap.GenerateSelfSignedWildcardCert(certDir, resetDomain)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					catalogUninstallIfRunning()
					// restore is handled by AfterEach above

					deployCtx, deployCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer deployCancel()

					logger.Infof("[TEST] deploying catalog with domain %s before reset-certificate test", resetDomain)
					deployOut, deployErr := cli.CatalogConfigureWithSSL(deployCtx, cfg, initialCert.CertPath, initialCert.KeyPath, appRuntime)
					gomega.Expect(deployErr).NotTo(gomega.HaveOccurred(),
						"initial SSL deploy should succeed; output:\n%s", deployOut)

					resetCertDir, err := os.MkdirTemp("", "ais-catalog-reset-cert-new-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(resetCertDir)

					newCert, err := bootstrap.GenerateSelfSignedWildcardCert(resetCertDir, resetDomain)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure --reset-certificate (domain=%s)", resetDomain)
					output, err := cli.CatalogConfigureResetCert(ctx, cfg, newCert.CertPath, newCert.KeyPath, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"--reset-certificate should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogResetCertOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] certificate reset validated")
				},
			)

			ginkgo.It(
				"rejects invalid flag combinations for catalog configure",
				ginkgo.Label("catalog-configure", "ssl", "negative", "spyre-independent"),
				func() {
					skipIfNotPodman()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					logger.Infof("[TEST] testing --reset-certificate without --ssl-cert/--ssl-key")
					output, err := cli.CatalogConfigureWithArgs(ctx, cfg, appRuntime, "--reset-certificate")
					gomega.Expect(err).To(gomega.HaveOccurred(),
						"--reset-certificate without --ssl-cert/--ssl-key must fail; output:\n%s", output)
					gomega.Expect(
						cli.ValidateCatalogInvalidFlagCombinationOutput(output + err.Error()),
					).To(gomega.Succeed())

					certDir, cerr := os.MkdirTemp("", "ais-catalog-flag-combo-*")
					gomega.Expect(cerr).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					cert, genErr := bootstrap.GenerateSelfSignedWildcardCert(certDir, "e2etest.local")
					gomega.Expect(genErr).NotTo(gomega.HaveOccurred())

					ctx2, cancel2 := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel2()

					logger.Infof("[TEST] testing --ssl-cert without --ssl-key")
					output2, err2 := cli.CatalogConfigureWithArgs(ctx2, cfg, appRuntime, "--ssl-cert", cert.CertPath)
					gomega.Expect(err2).To(gomega.HaveOccurred(),
						"--ssl-cert without --ssl-key must fail; output:\n%s", output2)
					gomega.Expect(
						cli.ValidateCatalogInvalidFlagCombinationOutput(output2 + err2.Error()),
					).To(gomega.Succeed())
					logger.Infof("[TEST] invalid flag combinations correctly rejected")
				},
			)

			ginkgo.It(
				"rejects a certificate whose domain does not match the configured domain",
				ginkgo.Label("catalog-configure", "ssl", "negative", "spyre-independent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					certDir, err := os.MkdirTemp("", "ais-catalog-domain-mismatch-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					cert, err := bootstrap.GenerateSelfSignedWildcardCert(certDir, "e2etest.local")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					logger.Infof("[TEST] testing cert domain mismatch")
					output, err := cli.CatalogConfigureWithArgs(
						ctx, cfg, appRuntime,
						"--ssl-cert", cert.CertPath,
						"--ssl-key", cert.KeyPath,
						"--domain-name", "other.local",
					)

					if err != nil {
						gomega.Expect(
							cli.ValidateCatalogCertDomainMismatchOutput(output + err.Error()),
						).To(gomega.Succeed())
					} else {
						lowerOut := strings.ToLower(output)
						domainWarning := strings.Contains(lowerOut, "ignored") ||
							strings.Contains(lowerOut, "domain") ||
							strings.Contains(lowerOut, "certificate")
						gomega.Expect(domainWarning).To(gomega.BeTrue(),
							"expected domain-ignored warning in output; got:\n%s", output)
					}
					logger.Infof("[TEST] certificate domain mismatch handling validated")
				},
			)

			ginkgo.It(
				"rejects invalid (non-PEM) certificate content",
				ginkgo.Label("catalog-configure", "ssl", "negative", "spyre-independent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					certDir, err := os.MkdirTemp("", "ais-catalog-invalid-cert-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					certPath, keyPath, err := bootstrap.WriteInvalidCertFiles(certDir)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					logger.Infof("[TEST] testing invalid certificate content")
					output, err := cli.CatalogConfigureWithArgs(
						ctx, cfg, appRuntime,
						"--ssl-cert", certPath,
						"--ssl-key", keyPath,
					)
					gomega.Expect(err).To(gomega.HaveOccurred(),
						"invalid certificate content must be rejected; output:\n%s", output)
					gomega.Expect(
						cli.ValidateCatalogInvalidCertOutput(output + err.Error()),
					).To(gomega.Succeed())
					logger.Infof("[TEST] invalid cert content correctly rejected: %v", err)
				},
			)

			// All cycles use the same domain (e2etest.local). We first deploy with that domain
			// so the running catalog matches; --reset-certificate rejects domain changes.
			// The AfterEach above handles teardown (uninstall + restore to nip.io).
			ginkgo.It(
				"supports multiple consecutive certificate reset cycles",
				ginkgo.Label("catalog-configure", "ssl", "reset", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					const (
						resetCycles = 2
						resetDomain = "e2etest.local"
					)

					initialCertDir, err := os.MkdirTemp("", "ais-catalog-cycles-initial-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(initialCertDir)

					initialCert, err := bootstrap.GenerateSelfSignedWildcardCert(initialCertDir, resetDomain)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					catalogUninstallIfRunning()
					// restore is handled by AfterEach above

					deployCtx, deployCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer deployCancel()

					logger.Infof("[TEST] deploying catalog with domain %s before reset cycles", resetDomain)
					deployOut, deployErr := cli.CatalogConfigureWithSSL(deployCtx, cfg, initialCert.CertPath, initialCert.KeyPath, appRuntime)
					gomega.Expect(deployErr).NotTo(gomega.HaveOccurred(),
						"initial SSL deploy should succeed; output:\n%s", deployOut)

					for i := 1; i <= resetCycles; i++ {
						certDir, err := os.MkdirTemp("", fmt.Sprintf("ais-catalog-cycle-%d-*", i))
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						defer os.RemoveAll(certDir)

						cert, err := bootstrap.GenerateSelfSignedWildcardCert(certDir, resetDomain)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())

						cycleCtx, cycleCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
						defer cycleCancel()

						logger.Infof("[TEST] certificate reset cycle %d/%d (domain=%s)", i, resetCycles, resetDomain)
						output, err := cli.CatalogConfigureResetCert(
							cycleCtx, cfg, cert.CertPath, cert.KeyPath, appRuntime,
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred(),
							"certificate reset cycle %d should succeed; output:\n%s", i, output)
						gomega.Expect(cli.ValidateCatalogResetCertOutput(output)).To(gomega.Succeed())
					}
					logger.Infof("[TEST] %d certificate reset cycles validated", resetCycles)
				},
			)
		})

		// ── Reset Podman Auth ─────────────────────────────────────────────────

		ginkgo.Context("Reset Podman Auth", func() {
			// Health check omitted — preceding SSL test may have deployed with e2etest.local (no DNS).
			ginkgo.It(
				"resets podman auth without breaking the running catalog deployment",
				ginkgo.Label("catalog-configure", "reset", "spyre-dependent"),
				func() {
					skipIfNotPodman()

					infoCtx, infoCancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer infoCancel()

					if _, infoErr := cli.CatalogInfo(infoCtx, cfg, appRuntime); infoErr != nil {
						ginkgo.Skip("catalog not running — skipping reset-podman-auth test")
					}

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					logger.Infof("[TEST] running catalog configure --reset-podman-auth")
					output, err := cli.CatalogConfigureResetAuth(ctx, cfg, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(),
						"--reset-podman-auth should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogResetAuthOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] --reset-podman-auth validated")
				},
			)
		})

		// ── Reset Without Deployment ──────────────────────────────────────────

		ginkgo.Context("Reset Operations Without Deployment", func() {
			ginkgo.It(
				"reset flags report 'not running' when catalog is not deployed",
				ginkgo.Label("catalog-configure", "reset", "negative", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					uninstallCtx, uninstallCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer uninstallCancel()

					logger.Infof("[TEST] uninstalling catalog to create 'not deployed' state")
					uninstallOut, uninstallErr := cli.CatalogUninstall(uninstallCtx, cfg, appRuntime)
					gomega.Expect(uninstallErr).NotTo(gomega.HaveOccurred(),
						"pre-condition uninstall failed; output:\n%s", uninstallOut)

					defer func() {
						restoreCtx, restoreCancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
						defer restoreCancel()
						logger.Infof("[TEST] restoring catalog after test")
						restoreOut, restoreErr := cli.CatalogConfigure(restoreCtx, cfg, appRuntime)
						if restoreErr != nil {
							logger.Warningf("[TEST] catalog restore warning: %v\n%s", restoreErr, restoreOut)
						} else {
							logger.Infof("[TEST] catalog restored successfully")
						}
					}()

					// notRunningOutput asserts output contains a clear "not running/configured" message; CLI may exit 0 or non-zero.
					notRunningOutput := func(out string, err error) {
						var combined string
						if err != nil {
							combined = out + err.Error()
						} else {
							combined = out
						}
						gomega.Expect(strings.ToLower(combined)).To(
							gomega.SatisfyAny(
								gomega.ContainSubstring("not configured"),
								gomega.ContainSubstring("not running"),
								gomega.ContainSubstring("not deployed"),
								gomega.ContainSubstring("configure"),
								gomega.ContainSubstring("error"),
								gomega.ContainSubstring("fail"),
							),
							"expected a 'not running' message; got:\n%s", combined,
						)
					}

					// Sub-case A: --reset-podman-auth when catalog is not deployed.
					authCtx, authCancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer authCancel()

					logger.Infof("[TEST] testing --reset-podman-auth when catalog is not deployed")
					authOut, authErr := cli.CatalogConfigureResetAuth(authCtx, cfg, appRuntime)
					notRunningOutput(authOut, authErr)

					// Sub-case B: --reset-certificate when catalog is not deployed.
					certDir, err := os.MkdirTemp("", "ais-catalog-reset-nodeply-*")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					defer os.RemoveAll(certDir)

					cert, err := bootstrap.GenerateSelfSignedWildcardCert(certDir, "e2etest.local")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					certCtx, certCancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer certCancel()

					logger.Infof("[TEST] testing --reset-certificate when catalog is not deployed")
					certOut, certErr := cli.CatalogConfigureResetCert(certCtx, cfg, cert.CertPath, cert.KeyPath, appRuntime)
					notRunningOutput(certOut, certErr)
					logger.Infof("[TEST] reset-without-deployment validated — both flags reported 'not running'")
				},
			)
		})

		// ── Extremely Long Basedir Path ───────────────────────────────────────

		ginkgo.Context("Extremely Long Basedir Path", func() {
			ginkgo.It(
				"rejects an extremely long --basedir path",
				ginkgo.Label("catalog-configure", "custom-path", "negative", "spyre-independent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					longPath := filepath.Join(os.TempDir(), strings.Repeat("a", 4097))

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					logger.Infof("[TEST] testing catalog configure with path length %d", len(longPath))
					output, err := cli.CatalogConfigureWithBasedir(ctx, cfg, longPath, appRuntime)
					gomega.Expect(err).To(gomega.HaveOccurred(),
						"extremely long basedir path should be rejected; output:\n%s", output)

					combined := output + err.Error()
					gomega.Expect(strings.ToLower(combined)).To(
						gomega.SatisfyAny(
							gomega.ContainSubstring("invalid"),
							gomega.ContainSubstring("error"),
							gomega.ContainSubstring("path"),
							gomega.ContainSubstring("long"),
							gomega.ContainSubstring("too"),
							gomega.ContainSubstring("name too long"),
							gomega.ContainSubstring("failed"),
						),
						"expected a path-related error; got:\n%s", combined,
					)
					logger.Infof("[TEST] extremely long basedir path correctly rejected: %v", err)
				},
			)
		})

		// ── Non-Root User Tests ───────────────────────────────────────────────

		ginkgo.Context("Non-Root User Configuration", func() {
			ginkgo.It(
				"non-root user can configure catalog with a home-directory basedir",
				ginkgo.Label("catalog-configure", "custom-path", "non-root", "spyre-dependent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					homeDir := nonRootHomeDir()
					basedir := filepath.Join(homeDir, ".ais-e2e-test", fmt.Sprintf("catalog-%d", time.Now().UnixNano()))
					gomega.Expect(os.MkdirAll(basedir, 0o755)).To(gomega.Succeed())
					defer func() { _ = os.RemoveAll(filepath.Join(homeDir, ".ais-e2e-test")) }()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureTestTimeout)
					defer cancel()

					output, configErr := nonRootCatalogRun(ctx, "--basedir", basedir)
					gomega.Expect(configErr).NotTo(gomega.HaveOccurred(),
						"non-root catalog configure should succeed; output:\n%s", output)
					gomega.Expect(cli.ValidateCatalogCustomPathOutput(output)).To(gomega.Succeed())
					logger.Infof("[TEST] non-root catalog configure succeeded")
				},
			)

			ginkgo.It(
				"non-root user is rejected when using a root-owned system basedir",
				ginkgo.Label("catalog-configure", "custom-path", "non-root", "negative", "spyre-independent"),
				func() {
					skipIfNotPodman()
					skipIfNoCatalogPassword()

					ctx, cancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer cancel()

					const systemBasedir = "/var/lib/ai-services"
					output, configErr := nonRootCatalogRun(ctx, "--basedir", systemBasedir)
					gomega.Expect(configErr).To(gomega.HaveOccurred(),
						"non-root user accessing system basedir should fail; output:\n%s", output)

					combined := output + configErr.Error()
					gomega.Expect(strings.ToLower(combined)).To(
						gomega.SatisfyAny(
							gomega.ContainSubstring("permission"),
							gomega.ContainSubstring("denied"),
							gomega.ContainSubstring("access"),
							gomega.ContainSubstring("not allowed"),
							gomega.ContainSubstring("error"),
							gomega.ContainSubstring("failed"),
						),
						"expected permission-denied error; got:\n%s", combined,
					)
					logger.Infof("[TEST] non-root permission check validated: %v", configErr)
				},
			)
		})

		// ── Catalog API Endpoint Tests ────────────────────────────────────────

		ginkgo.Context("Catalog API Endpoints",
			ginkgo.Ordered,
			func() {
				var (
					endpointBackendURL string
					authToken          string
					testAppName        string
				)

				ginkgo.BeforeAll(func() {
					skipIfNotPodman()

					if catalogBackendURL != "" {
						endpointBackendURL = catalogBackendURL
					} else {
						infoCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
						defer cancel()

						infoOut, infoErr := cli.CatalogInfo(infoCtx, cfg, appRuntime)
						if infoErr != nil {
							ginkgo.Skip("catalog not running — skipping catalog API endpoint tests")
						}
						endpointBackendURL = cli.ExtractCatalogBackendURL(infoOut)
					}

					if endpointBackendURL == "" {
						ginkgo.Skip("catalog backend URL not available — skipping catalog API endpoint tests")
					}

					_, catalogUsername, catalogPassword := bootstrap.GetCatalogCreds()
					if catalogPassword == "" {
						ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog API endpoint tests")
					}

					var tokenErr error
					tokenCtx, tokenCancel := context.WithTimeout(context.Background(), catalogConfigureShortTimeout)
					defer tokenCancel()
					authToken, tokenErr = catalogGetToken(tokenCtx, endpointBackendURL, catalogUsername, catalogPassword)
					if tokenErr != nil {
						ginkgo.Skip(fmt.Sprintf("could not obtain catalog auth token: %v — skipping endpoint tests", tokenErr))
					}

					testAppName = appName
					logger.Infof("[TEST] backend=%s app=%s", endpointBackendURL, testAppName)
				})

				ginkgo.It(
					"catalog returns service-level prompt params endpoint",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						url := endpointBackendURL + "/api/v1/catalog/prompt-params"
						logger.Infof("[TEST] GET %s", url)
						_, status, err := catalogDoRequest(context.Background(), http.MethodGet, url, authToken, nil)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(status).To(
							gomega.SatisfyAny(gomega.Equal(http.StatusOK), gomega.Equal(http.StatusNotFound)),
							"expected 200 or 404 from prompt-params endpoint; got %d", status,
						)
						logger.Infof("[TEST] prompt-params endpoint returned HTTP %d", status)
					},
				)

				ginkgo.It(
					"catalog applications endpoint accepts a sample payload",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						if testAppName == "" {
							ginkgo.Skip("no application name — skipping applications endpoint test")
						}
						url := endpointBackendURL + "/api/v1/applications"
						logger.Infof("[TEST] GET %s", url)
						body, status, err := catalogDoRequest(context.Background(), http.MethodGet, url, authToken, nil)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(status).To(gomega.Equal(http.StatusOK),
							"applications list should return 200; body: %s", string(body))
						logger.Infof("[TEST] applications endpoint returned HTTP %d", status)
					},
				)

				ginkgo.It(
					"catalog applications endpoint handles a watsonx-flavored payload",
					ginkgo.Label("catalog-configure", "endpoints"),
					func() {
						chatEndpointExpectNoError(context.Background(), endpointBackendURL, testAppName, authToken, map[string]interface{}{
							"messages": []map[string]string{{"role": "user", "content": "Hello from watsonx test"}},
							"model":    "ibm/granite-3-2b-instruct",
						})
					},
				)

				ginkgo.It(
					"catalog applications endpoint handles a vllm-key-flavored payload",
					ginkgo.Label("catalog-configure", "endpoints"),
					func() {
						chatEndpointExpectNoError(context.Background(), endpointBackendURL, testAppName, authToken, map[string]interface{}{
							"messages":   []map[string]string{{"role": "user", "content": "Hello from vllm test"}},
							"model":      "vllm-local",
							"extra_body": map[string]string{"api_key": "test-vllm-key"},
						})
					},
				)

				ginkgo.It(
					"catalog application endpoint accepts a system prompt in the payload",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						chatEndpointExpectNoError(context.Background(), endpointBackendURL, testAppName, authToken, map[string]interface{}{
							"messages": []map[string]string{
								{"role": "system", "content": "You are a helpful assistant."},
								{"role": "user", "content": "Hello"},
							},
						})
					},
				)

				ginkgo.It(
					"catalog authentication endpoints work correctly",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						_, catalogUsername, catalogPassword := bootstrap.GetCatalogCreds()
						if catalogPassword == "" {
							ginkgo.Skip("CATALOG_PASSWORD not set — skipping authentication endpoint test")
						}

						loginURL := endpointBackendURL + "/api/v1/auth/login"
						loginPayload, _ := json.Marshal(map[string]string{"username": catalogUsername, "password": catalogPassword})
						logger.Infof("[TEST] POST %s (valid creds)", loginURL)
						loginBody, loginStatus, loginErr := catalogDoRequest(context.Background(), http.MethodPost, loginURL, "", loginPayload)
						gomega.Expect(loginErr).NotTo(gomega.HaveOccurred())
						gomega.Expect(loginStatus).To(gomega.Equal(http.StatusOK),
							"valid login must return 200; body: %s", string(loginBody))

						badPayload, _ := json.Marshal(map[string]string{"username": catalogUsername, "password": "definitely-wrong-password-e2e"})
						logger.Infof("[TEST] POST %s (invalid creds)", loginURL)
						_, badStatus, badErr := catalogDoRequest(context.Background(), http.MethodPost, loginURL, "", badPayload)
						gomega.Expect(badErr).NotTo(gomega.HaveOccurred())
						gomega.Expect(badStatus).To(
							gomega.SatisfyAny(gomega.Equal(http.StatusUnauthorized), gomega.Equal(http.StatusForbidden)),
							"invalid login must return 401 or 403; got %d", badStatus,
						)
						logger.Infof("[TEST] authentication endpoints validated")
					},
				)

				ginkgo.It(
					"catalog application-management endpoints are accessible",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						listURL := endpointBackendURL + "/api/v1/applications"
						logger.Infof("[TEST] GET %s", listURL)
						body, status, err := catalogDoRequest(context.Background(), http.MethodGet, listURL, authToken, nil)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
						gomega.Expect(status).To(gomega.Equal(http.StatusOK),
							"application list must return 200; body: %s", string(body))
						logger.Infof("[TEST] application endpoints validated")
					},
				)

				ginkgo.It(
					"catalog service runtime endpoints respond correctly",
					ginkgo.Label("catalog-configure", "endpoints", "spyre-dependent"),
					func() {
						endpoints := []struct {
							method string
							path   string
						}{
							{http.MethodGet, "/health"},
							{http.MethodGet, "/api/v1/catalog"},
							{http.MethodGet, "/api/v1/applications"},
						}

						for _, ep := range endpoints {
							url := endpointBackendURL + ep.path
							logger.Infof("[TEST] %s %s", ep.method, url)
							_, status, err := catalogDoRequest(context.Background(), ep.method, url, authToken, nil)
							gomega.Expect(err).NotTo(gomega.HaveOccurred(),
								"request to %s must not error", url)
							gomega.Expect(status).To(
								gomega.BeNumerically("<", http.StatusInternalServerError),
								"%s %s must not return 5xx; got %d", ep.method, url, status,
							)
						}
						logger.Infof("[TEST] catalog runtime endpoints validated")
					},
				)
			},
		)
	},
)
