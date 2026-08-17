// This file covers catalog FAILURE scenarios — the counterpart to the
// success-path catalog tests in e2e_suite_test.go (Bootstrap Steps context).
//
// Test cases
//  1. Missing required --server flag     – cobra required-flag enforcement
//  2. Malformed (non-HTTP/S) server URL  – validateServerURL() in login.go
//  3. `catalog whoami` without prior login – client.New() finds no stored creds
//  4. Unpaired SSL flags (cert without key) – checkSSLFlagsPaired() in configure.go
//  5. Out-of-range --https-port value    – validateConfigureFlags() in configure.go
//
// Runtime compatibility
//
//  Tests 1, 2, 3  Run on BOTH podman and openshift runtimes.
//                 They test cobra / CLI flag validation that fires before any
//                 runtime-specific code is reached.
//                 NOTE: when --runtime=podman is passed on a non-linux/ppc64le
//                 machine, CheckPodmanPlatformSupport() fires in PreRunE.
//                 Test 1 is still safe (cobra required-flag check fires before
//                 PreRunE).  Tests 2 and 3 should therefore be run with
//                 --runtime=openshift on developer machines, and with
//                 --runtime=podman only in CI on actual ppc64le LPARs.
//
//  Tests 4, 5     Podman only — `catalog configure` is not yet supported on
//                 OpenShift (internal/pkg/catalog/cli/configure/common.go:30
//                 returns "openshift runtime is not yet supported for catalog
//                 configure").  Both tests skip automatically when appRuntime
//                 is not "podman".
//
// Labels
//
//	failure-test      – all tests in this file (umbrella label, shared with all failure suites)
//	catalog-failure   – all tests in this file (domain label)
//	catalog-login     – Tests 1, 2, 3
//	catalog-configure – Tests 4, 5
//
// Running ALL failure tests together (all three failure suites):
//
//	ginkgo -r --label-filter="failure-test" ./tests/e2e
//
// Excluding ALL failure tests from the normal run:
//
//	ginkgo -r --label-filter="!failure-test" ./tests/e2e
//
// Running only catalog failure tests:
//
//	ginkgo -r --label-filter="catalog-failure" ./tests/e2e
//
// Running by sub-category:
//
//	ginkgo -r --label-filter="failure-test && catalog-login"     ./tests/e2e
//	ginkgo -r --label-filter="failure-test && catalog-configure" ./tests/e2e
package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// catalogFailureTestTimeout caps the time a single catalog failure test is
// allowed to run.  All five tests exercise pure CLI flag/config validation — no
// network I/O or Podman involvement — so 30 seconds is more than generous.
const catalogFailureTestTimeout = 30 * time.Second

// ─────────────────────────────────────────────────────────────────────────────
// Catalog Failure Scenarios
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("Catalog Failure Scenarios",
	// ginkgo.Ordered is intentionally NOT used here.  Each test is fully
	// self-contained and must not depend on the result of a preceding test.
	func() {

		// ── Default-exclusion guard ───────────────────────────────────────────
		//
		// Failure tests are skipped unless --run-failure-tests is explicitly
		// passed.  This mirrors the --app-name guard used by Language Support
		// Tests and prevents accidental execution during a normal suite run.
		ginkgo.BeforeEach(func() {
			if !runFailureTests {
				ginkgo.Skip(
					"[FAILURE-TEST][Catalog] Skipping — pass --run-failure-tests to opt in to failure test execution",
				)
			}
		})

		// ── Test 1: Missing required --server flag ────────────────────────────
		//
		// Rationale: `catalog login` declares --server as a required cobra flag.
		// An operator who forgets to supply it must receive an immediate, clear
		// rejection message rather than a cryptic runtime panic or misleading
		// network error.  This exercises the cobra required-flag enforcement
		// layer before any RunE code runs, so no catalog service is needed.
		//
		// Expected CLI output (cobra):
		//   "required flag(s) \"--server\" not set"
		ginkgo.Context("Catalog Login Failures",
			func() {
				ginkgo.It(
					"catalog login rejects invocation with missing --server flag",
					ginkgo.Label("failure-test", "catalog-failure", "catalog-login", "spyre-independent"),
					func() {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							catalogFailureTestTimeout,
						)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Catalog] Invoking catalog login without --server flag",
						)

						output, err := cli.CatalogLoginMissingServer(ctx, cfg, appRuntime)

						// ── Assertions ─────────────────────────────────────
						// 1. The command MUST fail.
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected catalog login to fail when --server is omitted, but it succeeded",
						)

						// 2. The error output must name the missing flag.
						//    Pass output+err.Error() — cobra may write to stderr only,
						//    which ends up in err.Error() when CombinedOutput loses it.
						gomega.Expect(
							cli.ValidateCatalogLoginMissingFlagOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly rejected missing --server flag. Error: %v",
							err,
						)
					},
				)

				// ── Test 2: Malformed server URL ──────────────────────────
				//
				// Rationale: The --server value must be a valid http/https URL.
				// Passing a non-HTTP scheme (e.g. "ftp://…" or a bare hostname)
				// should be rejected by validateServerURL() in login.go's PreRunE
				// before any password prompt, network call, or token storage occurs.
				//
				// Expected CLI output (login.go validateServerURL):
				//   "invalid --server URL %q: scheme must be http or https"
				ginkgo.It(
					"catalog login rejects a --server URL with invalid scheme",
					ginkgo.Label("failure-test", "catalog-failure", "catalog-login", "spyre-independent"),
					func() {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							catalogFailureTestTimeout,
						)
						defer cancel()

						// Use an ftp:// URL — clearly not http/https and will never
						// accidentally resolve to a real host.
						const badURL = "ftp://catalog.invalid.example.com:9999"

						logger.Infof(
							"[FAILURE-TEST][Catalog] Invoking catalog login with bad URL: %s",
							badURL,
						)

						output, err := cli.CatalogLoginInvalidURL(ctx, cfg, badURL, appRuntime)

						// ── Assertions ─────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected catalog login to fail with an invalid URL scheme, but it succeeded",
						)

						gomega.Expect(
							cli.ValidateCatalogLoginBadURLOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly rejected invalid URL scheme. Error: %v",
							err,
						)
					},
				)

				// ── Test 3: `catalog whoami` without prior login ──────────
				//
				// Rationale: `catalog whoami` calls client.New() which loads the
				// stored token file from the OS user config directory.  When no
				// login has ever been performed the file does not exist and the
				// command must fail with a meaningful error, not a panic.
				//
				// Mechanism: The subprocess is given a fresh, empty HOME / XDG_CONFIG_HOME
				// so os.UserConfigDir() resolves to a directory with no token file,
				// regardless of whether the test runner is already logged in.
				//
				// Expected CLI output:
				//   "no such file or directory" OR "not logged in" OR "credentials"
				ginkgo.It(
					"catalog whoami fails when no credentials are stored",
					ginkgo.Label("failure-test", "catalog-failure", "catalog-login", "spyre-independent"),
					func() {
						ctx, cancel := context.WithTimeout(
							context.Background(),
							catalogFailureTestTimeout,
						)
						defer cancel()

						// Create a fresh empty temp dir that the subprocess will see
						// as its HOME, guaranteeing no token file is present.
						emptyHome, err := os.MkdirTemp("", "ais-catalog-failure-test-*")
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Failed to create empty temp home dir for whoami test",
						)
						defer func() {
							if removeErr := os.RemoveAll(emptyHome); removeErr != nil {
								logger.Errorf(
									"[FAILURE-TEST][Catalog] Failed to remove temp home dir %s: %v",
									emptyHome, removeErr,
								)
							}
						}()

						logger.Infof(
							"[FAILURE-TEST][Catalog] Invoking catalog whoami with empty HOME=%s",
							emptyHome,
						)

						output, cmdErr := cli.CatalogWhoamiWithoutLogin(ctx, cfg, emptyHome, appRuntime)

						// ── Assertions ─────────────────────────────────────
						gomega.Expect(cmdErr).To(
							gomega.HaveOccurred(),
							"Expected catalog whoami to fail when no credentials exist, but it succeeded",
						)

						gomega.Expect(
							cli.ValidateCatalogWhoamiNotLoggedInOutput(output+cmdErr.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly reported missing credentials. Error: %v",
							cmdErr,
						)
					},
				)
			},
		)

		// ── Tests 4 & 5: catalog configure input-validation failures ─────────
		//
		// Rationale: `catalog configure` is a destructive operation that deploys
		// or reconfigures catalog pods.  Bad flag combinations must be caught by
		// PreRunE before any deployment action runs.  Tests 4 and 5 verify the
		// two independent input-validation paths in configure.go.
		ginkgo.Context("Catalog Configure Failures",
			func() {
				// ── Test 4: Unpaired SSL flags ────────────────────────────
				//
				// Rationale: The --ssl-cert and --ssl-key flags must always be
				// used together.  Providing one without the other is a user error
				// that configure.go detects via checkSSLFlagsPaired() in PreRunE.
				//
				// Expected CLI output (configure.go checkSSLFlagsPaired):
				//   "--ssl-cert and --ssl-key must be used together"
				ginkgo.It(
					"catalog configure rejects --ssl-cert without --ssl-key",
					ginkgo.Label("failure-test", "catalog-failure", "catalog-configure", "spyre-independent"),
					func() {
						// catalog configure is not supported on OpenShift — the product
						// itself returns "openshift runtime is not yet supported for catalog
						// configure" (configure/common.go:30) before any flag validation
						// runs.  Skip on OpenShift to avoid asserting against that unrelated
						// error rather than the SSL-pairing error we actually want to test.
						if appRuntime != "podman" {
							ginkgo.Skip(
								"catalog configure is not yet supported on the openshift runtime — skipping SSL flag test",
							)
						}

						ctx, cancel := context.WithTimeout(
							context.Background(),
							catalogFailureTestTimeout,
						)
						defer cancel()

						// Any non-empty path string is sufficient; the pairing check
						// fires before the file is opened.
						const fakeCertPath = "/tmp/ais-catalog-failure-test-fake.pem"

						logger.Infof(
							"[FAILURE-TEST][Catalog] Invoking catalog configure with --ssl-cert only (no --ssl-key)",
						)

						output, err := cli.CatalogConfigureUnpairedSSL(ctx, cfg, fakeCertPath, appRuntime)

						// ── Assertions ─────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							"Expected catalog configure to fail with unpaired SSL flags, but it succeeded",
						)

						gomega.Expect(
							cli.ValidateCatalogConfigureUnpairedSSLOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly rejected unpaired SSL flags. Error: %v",
							err,
						)
					},
				)

				// ── Test 5: Out-of-range --https-port ─────────────────────
				//
				// Rationale: Valid TCP ports are 1–65535.  Passing 0 (or any
				// value outside that range) must be caught by
				// validateConfigureFlags() in PreRunE before any pods are touched.
				//
				// We test port 0 — an obviously invalid value that has no chance
				// of accidentally succeeding against a real system.
				//
				// Expected CLI output (configure.go validateConfigureFlags):
				//   "invalid HTTPS port 0: must be between 1 and 65535"
				ginkgo.It(
					"catalog configure rejects an out-of-range --https-port value",
					ginkgo.Label("failure-test", "catalog-failure", "catalog-configure", "spyre-independent"),
					func() {
						// Same reason as Test 4 — catalog configure is not supported on
						// OpenShift, so the port validation error we want to assert would
						// never be reached.  Skip on non-podman runtimes.
						if appRuntime != "podman" {
							ginkgo.Skip(
								"catalog configure is not yet supported on the openshift runtime — skipping port validation test",
							)
						}

						ctx, cancel := context.WithTimeout(
							context.Background(),
							catalogFailureTestTimeout,
						)
						defer cancel()

						const invalidPort = 0 // outside the valid 1–65535 range

						logger.Infof(
							"[FAILURE-TEST][Catalog] Invoking catalog configure with --https-port %d",
							invalidPort,
						)

						output, err := cli.CatalogConfigureInvalidPort(ctx, cfg, invalidPort, appRuntime)

						// ── Assertions ─────────────────────────────────────
						gomega.Expect(err).To(
							gomega.HaveOccurred(),
							fmt.Sprintf(
								"Expected catalog configure to fail with --https-port %d, but it succeeded",
								invalidPort,
							),
						)

						gomega.Expect(
							cli.ValidateCatalogConfigureInvalidPortOutput(output+err.Error()),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Catalog] Correctly rejected invalid port %d. Error: %v",
							invalidPort, err,
						)
					},
				)
			},
		)
	},
)

