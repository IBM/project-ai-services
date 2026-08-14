## AI Services — E2E Test Suite

## Purpose

This document explains how to run the End-to-End (E2E) test suite located under `ai-services/tests/e2e`, how to run the suite, and how to add new tests.

## Prerequisites

The Ginkgo test suite runs an end-to-end test which consists of setting up the machine with ai-services binary, checking for the
minimum number of Spyre cards installed, amongst other pre-flight checks.

- Go toolchain (the repository uses Go modules). Use the Go version listed in `ai-services/go.mod`.
- Git (to checkout branches or test fixtures).
- Podman (preferred runtime) — the suite checks for Podman and may install or skip some tests when Podman is not available. See `tests/e2e/bootstrap` for details.
- Set the required environment variables before running the suite.
- The golden dataset CSV file must be placed inside the `project-ai-services/test/golden/` directory. The filename should match the value provided in the `GOLDEN_DATASET_FILE` environment variable.
- Ginkgo CLI — tests can be run with `go test` or `ginkgo`.

## How to run tests locally

1. From the repository root, change into the `ai-services` folder:

   cd ai-services

2. To run the E2E suite follow either of the options below:
   1. Run using `go test`

      ```bash
      go test ./tests/e2e -v
      ```

      Notes:
      - The suite is implemented using Ginkgo v2 but is runnable via `go test` because the suite registers with the testing package.
      - Many E2E tests perform long-running operations (image pulls, application startup, ingestion). Expect tests to take many minutes (or longer) depending on environment and flags.

   2. Run using `make` (which uses `ginkgo cli` under the hood)

      ```bash
      make test

      make test-generate-report TEST_ARGS="--timeout=3h" APP_RUNTIME=<openshift/podman - default is podman>
      ```

      Notes:
      - This target runs all tests under `tests/e2e` using `ginkgo -r ./tests/e2e`
      - It can be customized by setting environment variables `TEST_ARGS` for example `make test TEST_ARGS="-v"`.
      - The `test-generate-report` runs the entire test and stores a JUnit XML report in `tests/e2e/reports/report-$(RUN_ID).xml`
      - The required build tags (`exclude_graphdriver_btrfs containers_image_openpgp remote`) are applied automatically via `TEST_BASE` in the Makefile.

   3. Run using the Ginkgo CLI

      > **Important:** always supply `-tags "$TAGS"` when invoking `ginkgo` directly. Without these tags the build will fail with `gpgme: build constraints exclude all Go files` because `containers/podman` pulls in a C-library binding that is excluded by these tags.

      ```bash
      ### install ginkgo
      go install github.com/onsi/ginkgo/v2/ginkgo@latest

      ### add the installation path to PATH
      export PATH=$PATH:$(go env GOPATH)/bin

      ### set build tags once — reuse in all ginkgo commands below
      export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"

      ### run the whole suite
      ginkgo -r -tags "$TAGS" \
        --timeout=3h ./tests/e2e -- --runtime=<openshift/podman - default is podman>

      ### to generate a junit report with ginkgo
      ginkgo -r -tags "$TAGS" \
        --timeout=3h --runtime=<openshift/podman - default is podman> \
        --junit-report=e2e-report.xml --output-dir=tests/e2e/reports \
        ./tests/e2e/...
      ```

## Environment variables to set before running tests

The test suite reads several environment variables. Many have sensible defaults, so set these before running the suite when required.

```bash
# Container registry credentials used by ai-services image pulls
export REGISTRY_URL="icr.io"
export REGISTRY_USER_NAME=myuser
export REGISTRY_PASSWORD=mypassword

# Red Hat registry credentials used to pull vLLM and the LLM judge image
export RH_REGISTRY_URL="registry.redhat.io"
export RH_REGISTRY_USER_NAME=<your redhat acc username>
export RH_REGISTRY_PASSWORD=<your redhat acc password>
export LLM_JUDGE_IMAGE="registry.io/example/vllm-judge:latest"
export LLM_CONTAINER_POLLING_INTERVAL=30s

# Golden dataset validation inputs
export GOLDEN_DATASET_FILE="filename.csv"
export RAG_ACCURACY_THRESHOLD=0.70

# LLM-as-a-judge model details
export LLM_JUDGE_MODEL_PATH="/var/lib/ai-services/models/"
export LLM_JUDGE_MODEL="Qwen/Qwen2.5-7B-Instruct"

# Optional application create params
export CREATE_PARAMS="reranker.vllm-cpu=true"   # use this for a 4-Spyre-card setup

# Catalog setup used by bootstrap and catalog login tests
export CATALOG_USERNAME="admin"
export CATALOG_PASSWORD=<your-catalog-admin-password>
export CATALOG_INSECURE=true

# Language Support Tests (TC-7 golden dataset validation — optional)
export GERMAN_GOLDEN_DATASET_FILE="german_golden.csv"
export FRENCH_GOLDEN_DATASET_FILE="french_golden.csv"
export ITALIAN_GOLDEN_DATASET_FILE="italian_golden.csv"
```

## Common E2E labels

Use Ginkgo label filters to run only the part of the suite you need.

| Label | Coverage |
|---|---|
| `golden-dataset-validation` | RAG golden dataset validation against an existing application |
| `digitization-tests` | Digitization API coverage against an existing or suite-created application |
| `similarity-tests` | Similarity API health and `/v1/similarity-search` behavior |
| `summarization-tests` | Asynchronous and synchronous summarization API coverage (requires `--template=summarize`) |
| `app-backup-restore` | Application backup and restore validation for OpenSearch and digitize data |
| `failure-test` | **Umbrella label** — all negative-path tests across bootstrap, catalog, and similarity. **Skipped by default** unless `--run-failure-tests` is passed; use `--label-filter="failure-test"` to further narrow once unlocked |
| `bootstrap-failure` | Bootstrap domain failure tests only (`bootstrap_failure_test.go`) — requires `--run-failure-tests` |
| `catalog-failure` | Catalog domain failure tests only (`catalog_failure_test.go`) — requires `--run-failure-tests` |
| `similarity-failure` | Similarity domain failure tests only (`similarity_failure_test.go`) — requires `--run-failure-tests` |

### Running Summarization Tests Only

```bash
# Using make (recommended — build tags applied automatically)
make test TEST_ARGS="--label-filter=summarization-tests --timeout=3h" \
  APP_NAME=<appname> APP_RUNTIME=podman

# Using go run (tags required — or set TAGS var as shown above)
export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"
go run github.com/onsi/ginkgo/v2/ginkgo \
  -tags "$TAGS" \
  --label-filter="summarization-tests" \
  --timeout=3h --v \
  . \
  -- -app-name=<appname> -template=summarize -runtime=podman

# Using ginkgo CLI directly
ginkgo -r -tags "$TAGS" \
  --label-filter="summarization-tests" --timeout=3h \
  ./tests/e2e -- --app-name=<appname> --template=summarize --runtime=podman
```

## Running Golden Dataset Validation Independently

The RAG Golden Dataset Validation can be executed independently from the full E2E lifecycle. This allows validating an already running RAG application without creating or deleting an application during the test run.

This mode is useful when:

- A RAG application is already deployed.
- You only want to validate model accuracy.
- You want to avoid image pulls, bootstrap, or provisioning steps.

## Prerequisites

- A RAG application must already be running.
- The application must be healthy.
- The application must expose an accessible endpoint.
- The golden dataset CSV file must be placed inside the `project-ai-services/test/golden/` directory. The filename should match the value provided in the `GOLDEN_DATASET_FILE` environment variable.
- The following environment variables must be set

```bash
export GOLDEN_DATASET_FILE="filename.csv"

export RAG_ACCURACY_THRESHOLD=0.70

export RH_REGISTRY_URL="registry.redhat.io"
export RH_REGISTRY_USER_NAME=<your redhat acc username>
export RH_REGISTRY_PASSWORD=<your redhat acc password>

export LLM_JUDGE_IMAGE="registry.io/example/vllm-judge:latest"
export LLM_JUDGE_MODEL_PATH="/var/lib/ai-services/models/"
export LLM_JUDGE_MODEL="Qwen/Qwen2.5-7B-Instruct"
export LLM_CONTAINER_POLLING_INTERVAL=30s
```

- Verify the application exists:

```
ai-services application info <app-name>
```

If this command fails, golden dataset validation will fail.

## Command to Run Golden Validation Only

```
make test TEST_ARGS="--label-filter=golden-dataset-validation" APP_NAME=<existing-app-name>
```

OR

```bash
ginkgo -r -tags "exclude_graphdriver_btrfs containers_image_openpgp remote" \
  ./tests/e2e \
  --label-filter=golden-dataset-validation \
  -- \
  --app-name=<existing-app-name>
```

## Running Digitization API Tests Independently

The Digitization API tests can be executed independently from the full E2E lifecycle. This allows validating an already running application without creating or deleting an application during the test run.

## Prerequisites

- A RAG application with digitize service must already be running.
- The application must be healthy.
- The digitize service FQDN can be obtained from `ai-services application info <app-name> --runtime <runtime>`.

- Verify the application exists:

```bash
ai-services application info <app-name> --runtime <runtime>
```

If this command fails, the test run will fail.

## Command to Run Digitization API Tests Only

```bash
make test TEST_ARGS="--label-filter=\"digitization-tests\" --timeout=2h" APP_NAME=<appname> APP_RUNTIME=<runtime>
```

OR

```bash
ginkgo -r -tags "exclude_graphdriver_btrfs containers_image_openpgp remote" \
  --label-filter="digitization-tests" --timeout=2h \
  ./tests/e2e -- --app-name=<appname> --runtime=<runtime>
```

## Running Similarity API Tests Independently

The Similarity API tests validate the `similarity-api` service after the application is up. They cover health checks and `/v1/similarity-search` behavior for dense, sparse, hybrid, rerank, and invalid-input scenarios.

## Prerequisites

- A RAG application with similarity service must already be running.
- The application must be healthy.
- Document ingestion or digitization ingestion must be possible so the similarity index contains test data.
- The similarity service FQDN can be obtained from `ai-services application info <app-name> --runtime <runtime>`.

## Command to Run Similarity API Tests Only

```bash
make test TEST_ARGS="--label-filter=\"similarity-tests\" --timeout=2h" APP_NAME=<appname> APP_RUNTIME=<runtime>
```

OR

```bash
ginkgo -r -tags "exclude_graphdriver_btrfs containers_image_openpgp remote" \
  --label-filter="similarity-tests" --timeout=2h \
  ./tests/e2e -- --app-name=<appname> --runtime=<runtime>
```

## Running Application Backup And Restore Tests

The backup and restore tests validate that application data survives a backup/restore cycle. The suite currently backs up and restores both `opensearch` and `digitize` data, then verifies:

- digitize jobs are restored
- digitize documents are restored
- RAG responses for known prompts match before and after restore

When `--app-name` is provided, the suite restores into a fresh sibling application name instead of immediately reusing the original name.

## Prerequisites

- A healthy application must be available, either suite-created or supplied with `--app-name`.
- Catalog access must be configured because the flow performs catalog login before backup and restore operations.
- The runtime must be able to pull required images before application creation or recreation.

## Command to Run Backup And Restore Tests Only

```bash
make test TEST_ARGS="--label-filter=\"app-backup-restore\" --timeout=3h" APP_NAME=<appname> APP_RUNTIME=<runtime>
```

OR

```bash
ginkgo -r -tags "exclude_graphdriver_btrfs containers_image_openpgp remote" \
  --label-filter="app-backup-restore" --timeout=3h \
  ./tests/e2e -- --app-name=<appname> --runtime=<runtime>
```

## Running Failure Tests

### Overview — why failure tests are excluded by default

The three failure test files exercise **intentional error paths** (wrong credentials, bad inputs, unreachable services). They are **not** part of the normal E2E suite run because:

- They require specific environment conditions (e.g. a reachable catalog server to test wrong-password rejection).
- They intentionally make commands fail, which would appear as unexpected failures in a normal run.
- They should be run deliberately, in isolation, as part of a dedicated failure-scenario validation pass.

Each failure file has a `BeforeEach` guard at the `Describe` level that calls `ginkgo.Skip` unless the `--run-failure-tests` flag is explicitly passed. This mirrors the `--app-name` guard used by Language Support Tests — **no flag, no execution, no accidents**.

The labels (`failure-test`, `bootstrap-failure`, `catalog-failure`, `similarity-failure`, and sub-labels) are retained for **precision targeting** once the flag unlocks execution.

### How the two mechanisms interact

```
Normal full suite run  (make test)
  └─ --run-failure-tests not passed
     └─ BeforeEach fires ginkgo.Skip on every failure It() → all 15 skipped ✓

Run ALL failure tests
  └─ pass --run-failure-tests → guard passes → all 15 run ✓

Run only one domain
  └─ pass --run-failure-tests + --label-filter="catalog-failure"
     └─ guard passes, label narrows to 5 catalog tests ✓

Run one sub-category
  └─ pass --run-failure-tests + --label-filter="failure-test && catalog-configure"
     └─ guard passes, label narrows to Tests 4 & 5 of catalog_failure_test.go ✓
```

### Label hierarchy

```
failure-test                           ← umbrella: selects ALL failure tests by label
  ├─ bootstrap-failure                 ← domain: bootstrap_failure_test.go (5 tests)
  │    ├─ registry                     ← sub: invalid registry credentials
  │    ├─ catalog                      ← sub: wrong catalog password + unreachable server
  │    └─ validation / spyre           ← sub: bootstrap validate failures
  ├─ catalog-failure                   ← domain: catalog_failure_test.go (5 tests)
  │    ├─ catalog-login                ← sub: missing flag, bad URL, whoami without login
  │    └─ catalog-configure            ← sub: unpaired SSL flags, invalid port
  └─ similarity-failure                ← domain: similarity_failure_test.go (5 tests)
       ├─ similarity-input             ← sub: empty query, invalid mode, top_k=0
       ├─ similarity-connectivity      ← sub: unreachable similarity API
       └─ similarity-readiness         ← sub: empty vector index (HTTP 503)
```

---

### Running the full E2E suite (failure tests excluded automatically)

No special flag or filter needed. Because `--run-failure-tests` is not passed, the `BeforeEach` guard skips all 15 failure tests automatically.

```bash
# Using make (recommended — build tags applied automatically)
make test

# Using make with a JUnit report
make test-generate-report TEST_ARGS="--timeout=3h" APP_RUNTIME=podman

# Using ginkgo CLI directly
export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"
ginkgo -r -tags "$TAGS" \
  --timeout=3h \
  ./tests/e2e -- --runtime=podman
```

> **Note**: the `--label-filter="!failure-test"` approach also works as a belt-and-braces
> exclusion if preferred, but it is no longer required — the `BeforeEach` guard handles
> exclusion automatically.

---

### Running ALL failure tests together

Pass `--run-failure-tests` after `--` to unlock all three failure suites in a single pass.

```bash
# Using make
make test TEST_ARGS="--timeout=10m" APP_RUNTIME=podman -- --run-failure-tests

# Using ginkgo CLI
export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"
ginkgo -r -tags "$TAGS" \
  --timeout=10m \
  ./tests/e2e -- --runtime=podman --run-failure-tests
```

---

### Running a single failure domain

Pass `--run-failure-tests` to unlock, then use a domain label to narrow.

```bash
export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"

# Bootstrap failure tests only (5 tests)
ginkgo -r -tags "$TAGS" --label-filter="bootstrap-failure" --timeout=5m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Catalog failure tests only (5 tests)
ginkgo -r -tags "$TAGS" --label-filter="catalog-failure" --timeout=3m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Similarity failure tests only (5 tests)
ginkgo -r -tags "$TAGS" --label-filter="similarity-failure" --timeout=3m \
  ./tests/e2e -- --app-name=<deployed-app-name> --runtime=podman --run-failure-tests
```

Or with `make test`:

```bash
make test TEST_ARGS="--label-filter=bootstrap-failure  --timeout=5m" APP_RUNTIME=podman         -- --run-failure-tests
make test TEST_ARGS="--label-filter=catalog-failure    --timeout=3m" APP_RUNTIME=podman         -- --run-failure-tests
make test TEST_ARGS="--label-filter=similarity-failure --timeout=3m" APP_RUNTIME=podman APP_NAME=<app> -- --run-failure-tests
```

---

### Running a failure sub-category

Pass `--run-failure-tests` to unlock, then use a sub-label to narrow to a specific category.

```bash
export TAGS="exclude_graphdriver_btrfs containers_image_openpgp remote"

# Bootstrap — registry authentication only (Test 1)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && registry"    --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Bootstrap — catalog credential / connectivity failures (Tests 2a, 2b)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && catalog"     --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Bootstrap — invalid --runtime flag only (Test 3)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && validation && spyre-independent" --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Bootstrap — missing Spyre accelerator card only (Test 4)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && spyre"       --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Catalog — login / whoami failures only (Tests 1, 2, 3)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && catalog-login"     --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Catalog — configure validation failures only (Tests 4, 5)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && catalog-configure" --timeout=2m \
  ./tests/e2e -- --runtime=podman --run-failure-tests

# Similarity — input validation failures only (Tests 1, 2, 3)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && similarity-input"        --timeout=2m \
  ./tests/e2e -- --app-name=<app> --runtime=podman --run-failure-tests

# Similarity — connectivity failure only (Test 4 — no deployed app needed)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && similarity-connectivity" --timeout=2m \
  ./tests/e2e -- --run-failure-tests

# Similarity — empty-index readiness failure (Test 5)
ginkgo -r -tags "$TAGS" --label-filter="failure-test && similarity-readiness"    --timeout=2m \
  ./tests/e2e -- --app-name=<app> --runtime=podman --run-failure-tests
```

---

### Environment variables required by failure tests

Failure tests reuse the same environment variables as the main suite. No additional variables are needed — the tests deliberately supply *wrong* values internally and only read registry/catalog URLs from the environment to know which endpoint to target.

```bash
export AI_SERVICES_BIN=<path to ai-services binary>   # required by all failure tests
export REGISTRY_URL="icr.io"                           # used to target the correct registry endpoint
export CATALOG_SERVER_URL="..."                        # optional — auto-discovered from 'catalog info' if absent
```

---

### Failure test labels reference

| Label | File | Tests |
|---|---|---|
| `failure-test` | all three failure files | All 15 failure tests — umbrella label |
| `bootstrap-failure` | `bootstrap_failure_test.go` | All 5 bootstrap failure tests |
| `catalog-failure` | `catalog_failure_test.go` | All 5 catalog failure tests |
| `similarity-failure` | `similarity_failure_test.go` | All 5 similarity failure tests |
| `failure-test && registry` | `bootstrap_failure_test.go` | Invalid registry credentials (Test 1) |
| `failure-test && catalog` | `bootstrap_failure_test.go` | Wrong catalog password + unreachable server (Tests 2a, 2b) |
| `failure-test && validation` | `bootstrap_failure_test.go` | `bootstrap validate` failures (Tests 3, 4) |
| `failure-test && spyre` | `bootstrap_failure_test.go` | Missing Spyre accelerator card (Test 4) |
| `failure-test && catalog-login` | `catalog_failure_test.go` | Missing flag, bad URL, whoami without login (Tests 1, 2, 3) |
| `failure-test && catalog-configure` | `catalog_failure_test.go` | Unpaired SSL, invalid port (Tests 4, 5) |
| `failure-test && similarity-input` | `similarity_failure_test.go` | Empty query, invalid mode, top_k=0 (Tests 1, 2, 3) |
| `failure-test && similarity-connectivity` | `similarity_failure_test.go` | Unreachable similarity API (Test 4) |
| `failure-test && similarity-readiness` | `similarity_failure_test.go` | Empty vector index HTTP 503 (Test 5) |

---

### Adding new failure tests

Follow the component-per-file convention:

- Bootstrap failures → `bootstrap_failure_test.go`
- Catalog failures → `catalog_failure_test.go`
- Similarity failures → `similarity_failure_test.go`
- Digitization failures → `digitization_failure_test.go` *(future)*
- Ingestion failures → `ingestion_failure_test.go` *(future)*

Each new failure `Describe` block **must** include the `BeforeEach` guard as its first statement:

```go
ginkgo.BeforeEach(func() {
    if !runFailureTests {
        ginkgo.Skip(
            "[FAILURE-TEST][Domain] Skipping — pass --run-failure-tests to opt in to failure test execution",
        )
    }
})
```

Each failure `It()` block **must**:
1. Carry **both** the `"failure-test"` umbrella label and its domain label (e.g. `"bootstrap-failure"`).
2. Carry at least one sub-category label (e.g. `"registry"`, `"catalog-login"`).
3. Assert `err` **is non-nil** (the command must fail).
4. Call the matching `ValidateXxxFailureOutput()` function in `cli/output.go` to verify the error message is actionable.
5. Clean up any environment changes in a `defer` block.

Example `It()` label decoration:

```go
ginkgo.It(
    "rejects X when Y",
    ginkgo.Label("failure-test", "bootstrap-failure", "registry", "spyre-independent"),
    func() { ... },
)
```

---

## Running Catalog Configure Tests

`catalog_configure_test.go` covers the full `catalog configure` / `catalog uninstall` command surface — custom base directories, SSL certificate deployment, certificate reset, podman auth reset, idempotency, flag validation, and live API endpoints.

**All tests in this file are podman-only.** They skip automatically on the OpenShift runtime.

### Environment variables required

```bash
export CATALOG_PASSWORD=<your-catalog-admin-password>   # required — tests skip if unset

# Registry credentials — used to pre-authenticate podman before image pulls
export REGISTRY_URL="icr.io"
export REGISTRY_USER_NAME=<registry-user>
export REGISTRY_PASSWORD=<registry-password>

# Optional — non-root user tests (skip if unset when running as root)
export NONROOT_USER=<username>                          # Linux user with no sudo privilege
```

### Run commands

```bash
# Run all catalog configure tests
CGO_ENABLED=0 GOFLAGS="-tags=containers_image_openpgp" \
  ginkgo -r --timeout=0 --label-filter="catalog-configure" \
  ./tests/e2e -- --runtime podman
```
---

## Running Language Support Tests

The Language Support Tests validate the chatbot pipeline for German (DE), French (FR) and Italian (IT) — including automatic language detection, PDF ingestion, RAG retrieval, and golden dataset accuracy.

The tests are labelled `language-tests` and can be run independently against an already running application.

### Fixture files required

| File | Location |
|------|----------|
| `german.pdf` | `ai-services/tests/e2e/ingestion/docs/german.pdf` |
| `french.pdf` | `ai-services/tests/e2e/ingestion/docs/french.pdf` |
| `italian.pdf` | `ai-services/tests/e2e/ingestion/docs/italian.pdf` |
| `german_golden.csv` | `test/golden/german_golden.csv` |
| `french_golden.csv` | `test/golden/french_golden.csv` |
| `italian_golden.csv` | `test/golden/italian_golden.csv` |

TC-3, TC-4 and TC-6 (smoke tests) run without any fixture files. TC-1, TC-2 and TC-5 require the PDF files. TC-7 requires the golden CSV files and LLM-as-Judge configuration.

### Environment variables for Language Support Tests

```bash
# Required for TC-7 golden dataset validation (optional — tests skip if unset)
export GERMAN_GOLDEN_DATASET_FILE="german_golden.csv"
export FRENCH_GOLDEN_DATASET_FILE="french_golden.csv"
export ITALIAN_GOLDEN_DATASET_FILE="italian_golden.csv"

# LLM-as-Judge (same vars as golden dataset validation — required for TC-7)
export LLM_JUDGE_IMAGE="registry.io/example/vllm-judge:latest"
export LLM_JUDGE_MODEL_PATH="/var/lib/ai-services/models/"
export LLM_JUDGE_MODEL="Qwen/Qwen2.5-7B-Instruct"
export LLM_JUDGE_PORT=8000
```

### Commands to run Language Support Tests

```bash
# Run all language tests (TC-1 through TC-7, TC-7 skipped until judge is set up)
make test TEST_ARGS="--label-filter=language-tests --timeout=3h" APP_NAME=<existing-app-name>

# Run only the smoke tests (TC-3, TC-4 and TC-6 — no PDFs or judge needed)
make test TEST_ARGS="--label-filter=language-smoke --timeout=30m" APP_NAME=<existing-app-name>

# Run only the golden dataset accuracy test (TC-7 — all three languages)
make test TEST_ARGS="--label-filter=language-golden --timeout=3h" APP_NAME=<existing-app-name>

# Using ginkgo CLI directly
ginkgo -r -tags "exclude_graphdriver_btrfs containers_image_openpgp remote" \
  --label-filter=language-tests --timeout=3h \
  ./tests/e2e -- --app-name=<existing-app-name>
```

## Adding new E2E tests

Add new test files under `ai-services/tests/e2e/` as standard Go test files (package `e2e`). The suite's entrypoint is `e2e_suite_test.go` which registers the Ginkgo suite.

1. Create a new `my_feature_test.go` file in `ai-services/tests/e2e`, for example `my_feature_test.go`.
2. Use Ginkgo and Gomega style already used in the repo:

```go package e2e

   import (
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
   )

   var _ = Describe("My Feature", func() {
       It("does something expected", func() {
           Expect(true).To(BeTrue())
       })
   })
```

3. Keep tests idempotent and self-cleaning: create resources with unique names (the suite already generates a `runID`) and ensure teardown removes created resources. Use existing helpers where possible (`tests/e2e/cli`, `tests/e2e/bootstrap`, `tests/e2e/cleanup`).

4. If the test depends on external services (images, models), document that in the test file header and consider adding timeouts or retries.

## Best practices and conventions

- Use the suite's context helpers: `bootstrap`, `cli`, `ingestion`, `podman`, etc. Reuse validation helpers under `tests/e2e` rather than reimplementing checks.
- Prefer short timeout values for unit-like checks and longer timeouts for operations that need time (image pulls, container startup).
- Use `By("...")` messages (Ginkgo) and `fmt.Printf` to produce helpful logs when tests fail.
- Use `Skip("reason")` when a test cannot run in the current environment (e.g., Podman missing).

## Maintaining test stability

- Keep external dependencies pinned where possible (image tags, model versions).
- Add retries for transient network operations using the `tests` helpers (retry.go).
- If tests become flaky, split them and add targeted diagnostics to capture state on failure.

## Project Structure (E2E)

Below is an accurate overview of the current `ai-services/tests/e2e` layout and the primary files you will interact with when adding or debugging E2E tests.

```text
ai-services/tests/e2e/
   ├─ e2e_suite_test.go              # Ginkgo suite entrypoint — BeforeSuite/AfterSuite and global test setup
   ├─ README.md                      # suite usage, labels, prerequisites, and structure
   ├─ nightly_run.sh                 # helper script for scheduled suite execution
   ├─ catalog_configure_test.go      # Catalog configure/uninstall/SSL/reset/endpoint tests (podman-only)
   ├─ language_e2e_test.go           # Language support tests (DE/FR/IT) — TC-1 through TC-7
   ├─ bootstrap_failure_test.go      # Bootstrap failure scenarios (registry, catalog, validation)
   ├─ bootstrap/                     # runtime preparation and bootstrap helpers
   │   ├─ bootstrap.go
   │   ├─ build.go
   │   ├─ certs.go                   # self-signed wildcard cert generator and invalid cert writer
   │   ├─ env.go
   │   └─ podman.go
   ├─ cleanup/                       # teardown helpers used by AfterSuite and tests
   │   └─ tear.go
   ├─ cli/                           # helpers to invoke the ai-services CLI and validate output
   │   ├─ output.go
   │   └─ runner.go
   ├─ common/                        # small reusable helpers used across tests (exec, files, JSON, retries)
   │   ├─ exec.go
   │   ├─ files.go
   │   ├─ json.go
   │   ├─ retry.go
   │   └─ vars.go
   ├─ config/                        # test configuration helpers
   │   └─ config.go
   ├─ digitization/                  # digitization api test helper functions
   │   ├─ digitize.go
   │   └─ digitize_lang.go           # language PDF path helpers and ingestion wrapper (DE/FR/IT)
   ├─ ingestion/                     # document ingestion helpers and test fixtures
   │   ├─ ingest.go
   │   ├─ wait.go
   │   └─ docs/                      # test documents for document ingestion and digitization
   │       ├─ german.pdf             # German language fixture (IBM Power product page)
   │       ├─ french.pdf             # French language fixture (IBM Power product page)
   │       └─ italian.pdf            # Italian language fixture (IBM Power product page)
   ├─ podman/                        # Podman verification helpers (containers, ports, etc.)
   │   └─ containers.go
   ├─ rag/                           # RAG-related test helpers
   │   ├─ evaluator.go
   │   ├─ golden.go
   │   ├─ judge.go
   │   └─ setup.go
   ├─ similarity/                    # similarity API request/response helpers
   │   └─ similarity.go
   └─ <other_test_files>             # add your `_test.go` files here (package `e2e`)
```
