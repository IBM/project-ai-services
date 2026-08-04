#!/bin/bash

# Perform clean-up
echo "Cleaning up existing repository folder"
rm -rf /root/nightly-run/project-ai-services

# Clone the repository
cd /root/nightly-run
echo "Cloning ai services repository"
git clone https://github.com/IBM/project-ai-services.git
echo "Repository clone successfully"

# Trigger the suite
cd project-ai-services/ai-services
go install github.com/onsi/ginkgo/v2/ginkgo@latest
export PATH=$PATH:$(go env GOPATH)/bin

echo "Triggering the E2E suite run"
# --timeout=0 disables the Go test suite-level timeout entirely.
# Individual spec and node timeouts (SpecTimeout/NodeTimeout decorators in
# e2e_suite_test.go) govern each step:
#   BeforeAll  — NodeTimeout(3h)  covers model download + ingestion + judge start
#   It (eval)  — SpecTimeout(3h)  covers 50-question evaluation loop
# This prevents the suite from being killed before the evaluation completes
# regardless of how slow the LLM is on this hardware.
TEST_OUTPUT=$(make test-generate-report TEST_ARGS="--timeout=0" DELETE_APP=true)

# Capture the output of the suite
echo "Output of E2E test run"
echo "$TEST_OUTPUT"
