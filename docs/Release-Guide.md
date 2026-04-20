# Release Guide

This document provides a comprehensive step-by-step guide for executing a release of the AI Services project.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Release Process](#release-process)
  - [1. Code Signing](#1-code-signing)
  - [2. Image Signing](#2-image-signing)
  - [3. Signature Verification](#3-signature-verification)
  - [4. Image Promotion](#4-image-promotion)
- [Additional Resources](#additional-resources)

## Overview

This guide outlines the complete release workflow, encompassing security validation, artifact signing, container image signing, verification procedures, and promotion to production registries.

## Prerequisites

Complete the following tasks before initiating a release:

### 1. Security Compliance

- **CVE Remediation**: Verify that all high severity Common Vulnerabilities and Exposures (CVEs) have been addressed in the codebase
  - Review the [Image Scanner GitHub Action](https://github.com/IBM/project-ai-services/actions/workflows/image-scanner.yml) logs to identify reported CVEs by trivy scanner in container images
  - Check security issues identified by the [Dependabot](https://github.com/IBM/project-ai-services/security/dependabot) scanner
  - Trigger the [Twistlock Scanner Jenkins pipeline](https://sys-powercloud-team-jenkins.swg-devops.com/job/ai-services/job/twist-lock-scan/) to scan container images and review the generated report
- **Security Review**: Conduct a thorough review and remediate any critical or high severity security findings

### 2. Compliance Reporting

- **OSCC Statistics**: Generate Open Source Component Compliance (OSCC) statistics
- **License Reports**: Generate color-coded reports of package licenses for compliance review by following the [documented steps](https://ibm.ent.box.com/notes/1821974877149?s=jnvmuk4d2lc0aqki5n5tukvq7lldqxe6)
- **Release Criteria**: Confirm that all package licenses are approved according to the [OSSC license guidelines](https://w3.ibm.com/w3publisher/ossc-process/resources/licenses)

## Release Process

### 1. Code Signing

Sign release artifacts to ensure authenticity and integrity.

**Pipeline URL**: [code-signing-service](https://sys-powercloud-team-jenkins.swg-devops.com/job/ai-services/job/code-signing-service/)

**Procedure**:

1. Navigate to the Jenkins pipeline and trigger a new build
2. Provide the release tag as an input parameter (e.g., `v1.0.0`)
3. Monitor the pipeline execution until completion
4. Download the generated public key from the job artifacts
5. Upload the public key to the GitHub release artifacts

### 2. Image Signing

Sign container images to establish trust and verify provenance.

**Pipeline URL**: [image-code-signing-service](https://sys-powercloud-team-jenkins.swg-devops.com/job/ai-services/job/image-code-signing-service/)

**Procedure**:

1. Navigate to the Jenkins pipeline and trigger a new build
2. Provide the following input parameters:
   - **Release Tag**: The version tag for the release (e.g., `v0.2.0`)
   - **Registry**: The target registry location (e.g., `icr.io/ai-services`)
3. The pipeline will automatically sign the IBM Cloud Registry (ICR) images associated with the release tag
4. Verify the signature has been uploaded to the GitHub release artifacts

### 3. Signature Verification

Validate image signatures using Cosign to ensure integrity.

**Procedure**:

1. Install and configure the Cosign tool if not already available
2. Verify the image signatures using the public key from the release artifacts
3. Refer to the [Verified Installation with Cosign](./INSTALLATION.md#verified-installation-with-cosign) section in the Installation Guide for detailed instructions


### 4. Image Promotion

Promote verified images from the private registry to the public registry.

**Procedure**:

1. Execute the image promotion script with appropriate parameters
2. Verify successful promotion by checking image availability in the public registry
3. Confirm image tags and metadata are correctly propagated

**Example**:
```bash
# Promote images using the promotion script
./hack/promote-images.sh \
  -s icr.io/ai-services-cicd/tools:v1.0.0 \
  -d icr.io/ai-services/tools:v1.0.0 \
  -p YOUR_API_KEY
```

---

**Important**: Ensure you have the necessary permissions and credentials to access Jenkins pipelines, container registries, and GitHub release management before initiating the release process. Contact your team administrator if you require access.
