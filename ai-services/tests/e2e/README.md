
## AI Services — E2E Test Suite

Purpose
-------

This document explains how to run the End-to-End (E2E) test suite located under `ai-services/tests/e2e`, how to run the suite, and how to add new tests.

Prerequisites
-------------

The Ginkgo test suite runs an end-to-end test which consists of setting up the machine with ai-services binary, checking for the 
minimum number of Spyre cards installed, amongst other pre-flight checks.

- Go toolchain (the repository uses Go modules). Use the Go version listed in `ai-services/go.mod`.
- Git (to checkout branches or test fixtures).
- Podman (preferred runtime) — the suite checks for Podman and may install or skip some tests when Podman is not available. See `tests/e2e/bootstrap` for details.
- Set your environment Variables values 
- Ginkgo CLI — tests can be run with `go test` or `ginkgo`.

How to run tests locally
------------------------

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

      make test-generate-report TEST_ARGS="--timeout=2h"
      ```

      Notes:
      - This target runs all tests under `tests/e2e` using `ginkgo -r ./tests/e2e`
      - It can be customized by setting environment variables `TEST_ARGS` for example `make test TEST_ARGS="-v"`.
      - The `test-generate-report`  runs the entire test and stores a JUnit XML report in `tests/e2e/reports/report-$(RUN_ID).xml` 

   3. Run using the Ginkgo CLI 
      ```bash
      ### install ginkgo 
      go install github.com/onsi/ginkgo/v2/ginkgo@latest

      ### run the whole suite
      ginkgo -r --timeout=2h ./tests/e2e

      ### to generate a junit report with ginkgo 
      ginkgo  -r --timeout=2h --junit-report=e2e-report.xml --output-dir=tests/e2e/reports ./tests/e2e/... 
      ```


Environment variables to set before running tests
-------------------------------------------------

The test suite reads several environment variables. Many have sensible defaults, so set these before running the suite when required.

```bash
# Container registry credentials (used for pulling images)
export REGISTRY_URL="icr.io"
export REGISTRY_USER_NAME=myuser
export REGISTRY_PASSWORD=mypassword

# Used to download vllm image 
export RH_REGISTRY_URL="registry.redhat.io"
export RH_REGISTRY_USER_NAME=<your redhat acc username>
export RH_REGISTRY_PASSWORD=<your redhat acc password>
export LLM_JUDGE_IMAGE="registry.io/example/vllm-judge:latest"
export LLM_CONTAINER_POLLING_INTERVAL=30s

# Exposed Ports
export RAG_BACKEND_PORT=5100
export RAG_UI_PORT=3100
export LLM_JUDGE_PORT=8000

# LLM as a judge model details
export LLM_JUDGE_MODEL_PATH="/var/lib/ai-services/models/"
export LLM_JUDGE_MODEL="Qwen/Qwen2.5-7B-Instruct"

# Expected Golden Dataset accuracy 
export RAG_ACCURACY_THRESHOLD=0.70 
```

Adding new E2E tests
---------------------

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


Best practices and conventions
-----------------------------

- Use the suite's context helpers: `bootstrap`, `cli`, `ingestion`, `podman`, etc. Reuse validation helpers under `tests/e2e` rather than reimplementing checks.
- Prefer short timeout values for unit-like checks and longer timeouts for operations that need time (image pulls, container startup).
- Use `By("...")` messages (Ginkgo) and `fmt.Printf` to produce helpful logs when tests fail.
- Use `Skip("reason")` when a test cannot run in the current environment (e.g., Podman missing).

Maintaining test stability
-------------------------

- Keep external dependencies pinned where possible (image tags, model versions).
- Add retries for transient network operations using the `tests` helpers (retry.go).
- If tests become flaky, split them and add targeted diagnostics to capture state on failure.


Project Structure (E2E)
--------------------------
Below is an accurate overview of the current `ai-services/tests/e2e` layout and the primary files you will interact with when adding or debugging E2E tests.

```text
ai-services/tests/e2e/
   ├─ e2e_suite_test.go           # Ginkgo suite entrypoint — BeforeSuite/AfterSuite and global test setup
   ├─ bootstrap/                  # runtime preparation and bootstrap helpers
   │   ├─ bootstrap.go
   │   ├─ build.go
   │   ├─ env.go
   │   └─ podman.go
   ├─ cleanup/                    # teardown helpers used by AfterSuite and tests
   │   └─ tear.go
   ├─ cli/                        # helpers to invoke the ai-services CLI and validate output
   │   ├─ output.go
   │   └─ runner.go
   ├─ common/                     # small reusable helpers used across tests (exec, files, logging, retries)
   │   ├─ exec.go
   │   ├─ files.go
   │   ├─ json.go
   │   ├─ logger.go
   │   ├─ retry.go
   │   └─ vars.go
   ├─ config/                     # test configuration helpers
   │   └─ config.go
   ├─ ingestion/                  # document ingestion helpers and test fixtures
   │   ├─ ingest.go
   │   ├─ wait.go
   │   └─ test_doc.pdf
   ├─ podman/                     # Podman verification helpers (containers, ports, etc.)
   │   └─ containers.go
   ├─ rag/                        # RAG-related test helpers (embeddings, setup, validate)
   |   ├─ evaluator.go
   |   ├─ golden.go
   |   ├─ judge.go
   │   ├─ setup.go
   ├─ reports/                   # generated test reports (JUnit XML, etc.) are stored here
   ├─ utils/                      # small additional utilities used by tests
   │   └─ json.go
   └─ <other_test_files>          # add your `_test.go` files here (package `e2e`)
```