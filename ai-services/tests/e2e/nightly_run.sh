#!/bin/bash

#Perform clean-up
echo "Cleaning up existing repository folder"
rm -rf /root/nightly-run/project-ai-services

#Clone the repository
cd /root/nightly-run
echo "Cloning ai services repository"
git clone https://github.com/IBM/project-ai-services.git
echo "Repository clone successfully"

#Trigger the suite
cd project-ai-services/ai-services
go install github.com/onsi/ginkgo/v2/ginkgo@latest
export PATH=$PATH:$(go env GOPATH)/bin

echo "Triggering the E2E suite run"
TEST_OUTPUT=$(make test-generate-report TEST_ARGS="--timeout=2h" DELETE_APP=true)

#Capture the output of the suite
echo "Output of E2E test run"
echo "$TEST_OUTPUT"
