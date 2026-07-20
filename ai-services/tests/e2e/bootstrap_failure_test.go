// This file covers bootstrap FAILURE scenarios — the counterpart to the
// success-path bootstrap tests in e2e_suite_test.go.
//
// Test cases
//  1. Invalid registry credentials      – podman login with wrong password
//  2. Catalog login failures            – wrong password AND unreachable server
//  3. Bootstrap validate failure        – simulate missing Podman prerequisite
//  4. Missing Spyre accelerator card    – bootstrap validate on LPAR without Spyre hardware
//
// Labels
//
//	failure-test   – all tests in this file
//	registry       – Test 1
//	catalog        – Test 2 (both sub-cases)
//	validation     – Test 3 and Test 4
//	spyre          – Test 4 specifically
//
// Running only failure tests:
//
//	ginkgo -r --label-filter="failure-test" ./tests/e2e
//
// Running a specific category:
//
//	ginkgo -r --label-filter="failure-test && registry"   ./tests/e2e
//	ginkgo -r --label-filter="failure-test && catalog"    ./tests/e2e
//	ginkgo -r --label-filter="failure-test && validation" ./tests/e2e
//	ginkgo -r --label-filter="failure-test && spyre"      ./tests/e2e
//
// Excluding failure tests from the normal run:
//
//	ginkgo -r --label-filter="!failure-test" ./tests/e2e
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// invalidRegistryUser and invalidRegistryPassword are deliberately wrong
// credentials used to trigger an authentication failure against the registry.
// They are kept as constants so future maintainers can see at a glance that
// the values are intentionally bogus.
const (
	invalidRegistryUser     = "invalid-user-bootstrap-failure-test"
	invalidRegistryPassword = "invalid-password-bootstrap-failure-test"

	// invalidCatalogPassword is used where the catalog server URL is real but
	// the credentials are wrong.
	invalidCatalogPassword = "wrong-catalog-password-bootstrap-failure-test"

	// unreachableCatalogURL points to a host that will never accept TCP
	// connections, so the CLI must handle a connection-refused / timeout error.
	unreachableCatalogURL = "https://catalog.invalid.bootstrap-failure-test.example.com:9999"

	// bootstrapFailureTestTimeout caps the time a single failure test is allowed
	// to run.  Most should complete within seconds; the generous budget covers
	// the catalog-unreachable test which must wait for a connection timeout.
	bootstrapFailureTestTimeout = 90 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Bootstrap Failure Scenarios
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("Bootstrap Failure Scenarios",
	// ginkgo.Ordered is intentionally NOT used here.  Each failure test is fully
	// self-contained and must not depend on the result of a preceding test.
	func() {

		// ── Test 1: Invalid Registry Credentials ─────────────────────────────
		//
		// Rationale: An operator who sets the wrong REGISTRY_PASSWORD should
		// receive an immediate, unambiguous authentication error rather than a
		// cryptic "image pull failed" later in the lifecycle.
		//
		// What we test:
		//   • The CLI exits non-zero when supplied invalid registry credentials.
		//   • The error output contains one of the well-known authentication
		//     failure strings emitted by Podman / Skopeo.
		//   • The test is skipped automatically when Podman is not present in the
		//     test environment (consistent with the non-blocking Podman check in
		//     BeforeSuite).
		ginkgo.Context("Registry Authentication Failures",
			func() {
				ginkgo.It(
					"fails gracefully with invalid registry credentials",
					ginkgo.Label("failure-test", "registry", "spyre-independent"),
					func() {
						// This test has only been validated on the podman runtime.
						// Skip explicitly on openshift until ported and verified.
						if appRuntime != "podman" {
							ginkgo.Skip(
								"registry failure test has only been validated on the podman runtime — skipping on " + appRuntime,
							)
						}

						ctx, cancel := context.WithTimeout(
							context.Background(),
							bootstrapFailureTestTimeout,
						)
						defer cancel()

						// Skip when Podman is not available — same guard used in
						// the success-path tests so the failure suite stays in sync.
						if err := bootstrap.CheckPodman(); err != nil {
							ginkgo.Skip(
								fmt.Sprintf(
									"Skipping registry failure test — Podman not available: %v",
									err,
								),
							)
						}

						registryURL, _, _ := bootstrap.GetPodManCreds()
						if registryURL == "" {
							// Fall back to ICR — the project's default registry —
							registryURL = "icr.io"
						}

						logger.Infof(
							"[FAILURE-TEST][Registry] Attempting login to %s with intentionally invalid credentials",
							registryURL,
						)

						output, err := attemptPodmanRegistryLogin(
							ctx,
							registryURL,
							invalidRegistryUser,
							invalidRegistryPassword,
						)

						// ── Assertions ──────────────────────────────────────
						// 1. The CLI / Podman command MUST fail.
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected registry login with invalid credentials to fail, but it succeeded",
						)

						// 2. The error output must contain an actionable message.
						gomega.Expect(
							cli.ValidateRegistryLoginFailureOutput(output),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Registry] Correctly rejected invalid credentials. Error: %v",
							err,
						)
					},
				)
			},
		)

		// ── Test 2: Catalog Service Failures ─────────────────────────────────
		//
		// Rationale: The catalog service is the gateway for podman-runtime
		// application deployments.  Two distinct failure modes must be handled:
		//
		//   2a. Wrong credentials   – server reachable, password wrong.
		//   2b. Unreachable server  – server does not exist / refused.
		//
		// Both must emit clear diagnostics.  2b also tests that the CLI does not
		// hang indefinitely waiting for a connection that will never arrive.
		ginkgo.Context("Catalog Service Failures",
			func() {
				// 2a ── Invalid catalog credentials ───────────────────────────
				ginkgo.It(
					"fails gracefully with invalid catalog credentials",
					ginkgo.Label("failure-test", "catalog", "spyre-independent"),
					func() {
						if appRuntime != "podman" {
							ginkgo.Skip(
								"catalog login is only exercised on the podman runtime",
							)
						}

						ctx, cancel := context.WithTimeout(
							context.Background(),
							bootstrapFailureTestTimeout,
						)
						defer cancel()

						// Resolve the catalog server URL.  The test requires a real
						// catalog endpoint to exercise authentication failure against —
						// a guessed URL would make the assertion non-deterministic.
						serverURL, _, _ := bootstrap.GetCatalogCreds()
						if serverURL == "" {
							// Try to discover a running catalog instance via catalog info.
							infoOut, infoErr := cli.CatalogInfo(ctx, cfg, appRuntime)
							if infoErr == nil {
								serverURL = cli.ExtractCatalogBackendURL(infoOut)
							}
						}
						if serverURL == "" {
							// No catalog URL resolvable — fail rather than skip so that
							// a misconfigured environment is surfaced clearly in CI.
							// The catalog service must be running for this test to be valid.
							// Remaining tests continue executing (ginkgo.Fail does not abort the suite).
							ginkgo.Fail(
								"[FAILURE-TEST][Catalog] Catalog credentials test cannot run: " +
									"no catalog URL available. " +
									"Set CATALOG_SERVER_URL or ensure the catalog service is running.",
							)
						}

						logger.Infof(
							"[FAILURE-TEST][Catalog] Attempting catalog login to %s with intentionally wrong password",
							serverURL,
						)

						output, err := cli.CatalogLogin(
							ctx,
							cfg,
							serverURL,
							"admin", // correct username — only the password is wrong
							invalidCatalogPassword,
							appRuntime,
							bootstrap.GetCatalogInsecure(),
						)

						// ── Assertions ──────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected catalog login with invalid password to fail, but it succeeded",
						)

						// CatalogLogin() wraps the raw CLI output inside err.Error() at
						// runner.go:915 ("catalog login failed: %w\n%s").  When the catalog
						// is not running the raw output is empty and the diagnostic text
						// lives only in err.Error().  Pass both so the validator always has
						// something to match against regardless of catalog state.
						gomega.Expect(
							cli.ValidateCatalogLoginFailureOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly rejected invalid catalog credentials. Error: %v",
							err,
						)
					},
				)

				// 2b ── Unreachable catalog server ─────────────────────────
				ginkgo.It(
					"fails gracefully when catalog server is unreachable",
					ginkgo.Label("failure-test", "catalog", "spyre-independent"),
					func() {
						if appRuntime != "podman" {
							ginkgo.Skip(
								"catalog login is only exercised on the podman runtime",
							)
						}

						// Allow extra time because the CLI may wait for a TCP
						// connection timeout before returning.
						ctx, cancel := context.WithTimeout(
							context.Background(),
							bootstrapFailureTestTimeout,
						)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Catalog] Attempting catalog login to unreachable server: %s",
							unreachableCatalogURL,
						)

						output, err := cli.CatalogLogin(
							ctx,
							cfg,
							unreachableCatalogURL,
							"admin",
							invalidCatalogPassword,
							appRuntime,
							true, // insecure=true — self-signed / no cert on a fake host
						)

						// ── Assertions ──────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected catalog login to unreachable server to fail, but it succeeded",
						)

						// Same reason as 2a — connectivity error text is inside err.Error().
						gomega.Expect(
							cli.ValidateCatalogUnreachableOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly handled unreachable catalog server. Error: %v",
							err,
						)
					},
				)
			},
		)

		
		// ── Test 3: Invalid Runtime Flag ─────────────────────────────────────
		//
		// Rationale: The --runtime flag is required for every bootstrap command.
		// Passing an unrecognised value must be rejected immediately by the CLI
		// before any validators or system checks run.  This is a pure CLI input-
		// validation failure that:
		//
		// Expected error from bootstrap.go:55:
		//   "invalid runtime type: <value> (must be 'podman' or 'openshift').
		//    Please specify runtime using --runtime flag"
		ginkgo.Context("Bootstrap Validation Failures",
			func() {
				ginkgo.It(
					"bootstrap validate rejects an invalid --runtime flag value",
					ginkgo.Label("failure-test", "validation", "spyre-independent"),
					func() {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							bootstrapFailureTestTimeout,
						)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Validate] Running bootstrap validate with invalid --runtime flag",
						)

						// Pass a clearly invalid runtime value.  The CLI's
						// bootstrapPersistentPreRunE hook must reject it before
						// any validator or system check runs.
						output, err := cli.BootstrapValidate(ctx, cfg, "invalid-runtime-value")

						// ── Assertions ──────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected bootstrap validate to reject invalid --runtime flag, but it succeeded",
						)

						gomega.Expect(
							cli.ValidateInvalidRuntimeOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Validate] bootstrap validate correctly rejected invalid runtime. Error: %v",
							err,
						)
					},
				)


				// ── Test 4: Missing Spyre Accelerator Card ────────────────
				//
				// Rationale: AI Services on IBM Power (podman runtime) requires at
				// least one IBM Spyre AI accelerator card to be physically attached to
				// the LPAR.  When `bootstrap validate` is run on an LPAR that has no
				// Spyre hardware the SpyreRule validator emits:
				//
				//   "IBM Spyre Accelerator is not attached to the LPAR"
				//
				// The existing success-path tests suppress this error and treat it as
				// a known acceptable state.  This test explicitly exercises the failure
				// path so the error message and exit code are verified in automation.
				//
				// Approach — lspci detection + conditional GHW_CHROOT:
				//
				//   lspci is first used to check whether Spyre cards (vendor 1014,
				//   device 06a7/06a8) are physically present on this machine.
				//
				//   • No cards:
				//     Run bootstrap validate directly — the validator naturally emits
				//     the "not attached" error without any environment manipulation.
				//
				//   • Cards present:
				//     Set GHW_CHROOT to an empty temp directory so the ghw library
				//     (used by IsApplicable() inside the validator) finds no PCI
				//     devices, making the test deterministic regardless of hardware.
				//     GHW_CHROOT is restored in a deferred cleanup.
				ginkgo.It(
					"bootstrap validate reports missing Spyre accelerator card",
					ginkgo.Label("failure-test", "validation", "spyre"),
					func() {
						if appRuntime != "podman" {
							ginkgo.Skip(
								"Spyre accelerator check is only performed for the podman runtime",
							)
						}
	
						ctx, cancel := context.WithTimeout(
							context.Background(),
							bootstrapFailureTestTimeout,
						)
						defer cancel()
	
						// Use lspci to determine whether Spyre cards are physically
						// present.  IsApplicable() checks count > 0, so the threshold
						// here is simply ≥ 1 card detected.
						present, cardCount := spyreCardsPresent()
						logger.Infof(
							"[FAILURE-TEST][Spyre] lspci detected %d Spyre card(s) on this machine",
							cardCount,
						)
	
						if present {
							// Cards are present — mask them from the validator using
							// GHW_CHROOT so the test is deterministic on any LPAR.
							emptyChrootDir, mkErr := os.MkdirTemp("", "ais-spyre-failure-test-*")
							if mkErr != nil {
								ginkgo.Fail(fmt.Sprintf(
									"[FAILURE-TEST][Spyre] Could not create empty chroot dir: %v",
									mkErr,
								))
							}
							defer func() {
								if removeErr := os.RemoveAll(emptyChrootDir); removeErr != nil {
									logger.Errorf(
										"[FAILURE-TEST][Spyre] Failed to remove temp chroot dir %s: %v",
										emptyChrootDir,
										removeErr,
									)
								}
							}()
	
							origGHWChroot := os.Getenv("GHW_CHROOT")
							defer func() {
								if origGHWChroot == "" {
									_ = os.Unsetenv("GHW_CHROOT")
								} else {
									_ = os.Setenv("GHW_CHROOT", origGHWChroot)
								}
								logger.Infof("[FAILURE-TEST][Spyre] GHW_CHROOT restored")
							}()
	
							if setErr := os.Setenv("GHW_CHROOT", emptyChrootDir); setErr != nil {
								ginkgo.Fail(fmt.Sprintf(
									"[FAILURE-TEST][Spyre] Could not set GHW_CHROOT: %v",
									setErr,
								))
							}
							logger.Infof(
								"[FAILURE-TEST][Spyre] %d Spyre card(s) present — GHW_CHROOT=%s to mask them from validator",
								cardCount,
								emptyChrootDir,
							)
						} else {
							logger.Infof(
								"[FAILURE-TEST][Spyre] 0 Spyre cards detected via lspci — running validate directly",
							)
						}
	
						output, err := cli.BootstrapValidate(ctx, cfg, appRuntime)
	
						// ── Assertions ──────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected bootstrap validate to report missing Spyre card, but it succeeded",
						)
	
						gomega.Expect(
							cli.ValidateSpyreAbsenceOutput(output),
						).To(gomega.Succeed())
	
						logger.Infof(
							"[FAILURE-TEST][Spyre] bootstrap validate correctly reported missing Spyre accelerator. Error: %v",
							err,
						)
					},
				)
			},
		)
	},
)

// ─────────────────────────────────────────────────────────────────────────────
// File-local helper functions
// ─────────────────────────────────────────────────────────────────────────────

// spyreCardsPresent returns whether at least one IBM Spyre PCI card is
// physically attached to the LPAR, and the total count detected.
// Uses lspci with the known vendor/device IDs:
//
//	vendor 1014 (IBM), device 06a7 (Rev1) or 06a8 (Rev2)
//
// This mirrors the detection used by accelerator/spyre/spyre.go:ListCards().
// A non-zero exit from lspci (no matches) is treated as "no cards present".
func spyreCardsPresent() (bool, int) {
	count := 0

	// Count Rev1 cards (06a7)
	out1, err1 := exec.Command("lspci", "-d", "1014:06a7").CombinedOutput()
	if err1 == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out1)), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
	}

	// Count Rev2 cards (06a8)
	out2, err2 := exec.Command("lspci", "-d", "1014:06a8").CombinedOutput()
	if err2 == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out2)), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
	}

	return count > 0, count
}

// attemptPodmanRegistryLogin runs `podman login` directly (not via the
// ai-services CLI) so that registry authentication can be tested independently
// of any catalog or application lifecycle state.
//
// It intentionally returns an error when the login fails — callers assert that
// the error is non-nil.
func attemptPodmanRegistryLogin(
	ctx context.Context,
	registryURL string,
	username string,
	password string,
) (string, error) {
	// Locate the podman binary — if it is not available the test should have
	// been skipped by the CheckPodman() guard above, but be defensive.
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return "", fmt.Errorf("podman not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(
		ctx,
		podmanPath,
		"login",
		registryURL,
		"--username", username,
		"--password", password,
		// Disable TLS verification so the test works in CI environments
		// where registry certs may be self-signed.  We are testing
		// authentication failure, not TLS validation.
		"--tls-verify=false",
	)

	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		logger.Infof("[FAILURE-TEST][Registry] podman login failed (expected): %v", err)
		return output, fmt.Errorf("podman login failed: %w\n%s", err, output)
	}

	return output, nil
}
