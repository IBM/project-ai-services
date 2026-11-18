# OpenSearch (ppc64le) Image Pipeline

This Jenkins pipeline builds OpenSearch from source for `ppc64le`, packages it as a container image, tests it, scans it, and pushes it to IBM Container Registry (ICR).

The pipeline definition is the [`Jenkinsfile`](./Jenkinsfile) in this directory.

## Before you start

Check that you have all of the following:

- Access to the Jenkins server (`https://sys-powercloud-team-jenkins.swg-devops.com/`). If you can't log in or can't see the `ai-services` folder, ask your team administrator for access.
- Permission to run builds on the OpenSearch job. Look for the **Build with Parameters** link on the job page.
- The OpenSearch version you want to build, for example `3.8.0`. The version must meet both conditions:
  - It is a **git tag** in [opensearch-project/opensearch-build](https://github.com/opensearch-project/opensearch-build/tags).
  - A matching Dockerfile folder and patch file exist in the build-scripts repo:
    - `o/opensearch-project-opensearch-build/Dockerfiles/<version>_ubi9.7/`
    - `o/opensearch-project-opensearch-build/Dockerfiles/<version>_ubi9.7/ppc64le-<version>-ai-services.patch`

If the folder or patch doesn't exist for your version, the build fails in the **Build** stage. Add them to build-scripts first.

## Run the pipeline

1. Open the OpenSearch job in Jenkins (under the `ai-services` folder).
2. Click **Build with Parameters**.
3. Fill in the parameters (see the table below). At minimum, set `OPENSEARCH_VERSION`.
4. Click **Build**.
5. Click the new build number under **Build History**, then **Console Output**, to watch progress.

The **Build** stage (compiling OpenSearch and its plugins) is the longest part. Expect the whole run to take a long time, and check the previous builds on the job page for the real duration.

### Parameters

| Parameter | Required | Default | What it does |
| --- | --- | --- | --- |
| `OPENSEARCH_VERSION` | Yes | empty | The OpenSearch version to build, for example `3.8.0`. Must be a tag in `opensearch-build`. |
| `BUILD_SCRIPTS_REPO` | No | `https://github.com/ppc64le/build-scripts.git` | The build-scripts repository to clone. Keep the default for normal builds. Change it only to test your own fork, for example `https://github.com/<user>/build-scripts.git`. Must be an HTTPS GitHub URL. |
| `BUILD_SCRIPTS_BRANCH` | No | `master` | The branch of that repository to check out. |

### Which options should I choose?

| I want to... | `OPENSEARCH_VERSION` | `BUILD_SCRIPTS_REPO` | `BUILD_SCRIPTS_BRANCH` |
| --- | --- | --- | --- |
| Build a released version | `3.8.0` | default | default |
| Test my changes to the Dockerfile or patch | `3.8.0` | your fork URL | `my-branch` |

Every successful build pushes an image, including builds from a fork. Use the build number in the tag to tell them apart.

## What the pipeline does

The pipeline runs on the Jenkins agent labelled `onprem-ai-services-machine`. Only one build can run at a time (`disableConcurrentBuilds`). If you start a second build, it waits in the queue.

| # | Stage | What happens | Fails the build? |
| --- | --- | --- | --- |
| 1 | **Prepare** | Cleans the workspace. Checks out this repo, `opensearch-build` (at the version tag), and `build-scripts`. | Yes |
| 2 | **Build** | Builds the build-environment image. Applies the ppc64le patch. Builds OpenSearch and its plugins inside a Podman container and assembles the tarball. | Yes |
| 3 | **Build Container Image** | Builds the final OpenSearch container image from the tarball. | Yes |
| 4 | **Validate** | Starts the image, checks the cluster is healthy, checks the version, and runs [`test/opensearch/validate.sh`](../../../test/opensearch/validate.sh) (the AI Services RAG usage test). | **No.** A failure marks the stage unstable (yellow) and the push still happens. |
| 5 | **Security Scan** | Runs Trivy for CRITICAL and HIGH vulnerabilities (unfixed ones are ignored). Saves a JSON and a text report. | Yes, if the scan can't run |
| 6 | **Push to ICR** | Pushes the image to ICR. | Yes |

Plugins built with OpenSearch: `common-utils`, `opensearch-remote-metadata-sdk`, `job-scheduler`, `security`, `custom-codecs`, `k-NN`, `ml-commons`, `neural-search`, `opensearch-observability`, `index-management`.

## What you get

### Container image

One image is pushed per successful build:

`icr.io/ai-services-cicd/opensearch:<version>-ppc64le-<build number>`

Example: `icr.io/ai-services-cicd/opensearch:3.8.0-ppc64le-12`

### Files on the Jenkins build page

Under **Build Artifacts**:

- `trivy-report/trivy-results.txt` - human-readable vulnerability report.
- `trivy-report/trivy-results.json` - the same report in JSON.
- `opensearch-build/tar/dist/opensearch/*` - the assembled OpenSearch tarball.

The tarball is also copied to `/var/jenkins/artifacts/opensearch/` on the Jenkins agent.

## Check that the build worked

1. The build is **blue or green** (success) or **yellow** (unstable). Red means failed.
2. The last line of the console output reads `OpenSearch <version> (ppc64le) image successfully published to ...`.
3. If the build is yellow, open the **Validate** stage log. The image was still pushed, but review the failure before using it.
4. Open `trivy-report/trivy-results.txt` and review the CRITICAL and HIGH findings.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Fails in **Prepare** with `couldn't find remote ref refs/tags/<version>` | The version isn't a tag in `opensearch-build`. | Check the version spelling at the [tags page](https://github.com/opensearch-project/opensearch-build/tags). |
| Fails in **Prepare** with `BUILD_SCRIPTS_REPO must be an HTTPS GitHub URL` | The repository URL isn't in the form `https://github.com/<user>/<repo>.git`. | Fix the URL. |
| Fails in **Prepare** while checking out `build-scripts` | The `BUILD_SCRIPTS_BRANCH` doesn't exist in `BUILD_SCRIPTS_REPO`, or the repo is private. | Fix the branch name or the repo URL. |
| Fails in **Build** with `No such file or directory` for the Dockerfile or patch | No `<version>_ubi9.7` folder or patch file in build-scripts. | Add them to build-scripts, then rebuild. |
| Fails in **Build** with `git apply` errors | The patch doesn't apply to this version. | Regenerate the patch for this version. |
| **Validate** is yellow | The smoke test or `validate.sh` failed. | Read the stage log. The image was still pushed. |
| Fails in **Push to ICR** with an authentication error | The ICR credentials in Jenkins are expired or missing. | Ask the Jenkins administrator to refresh `icr-iam-creds`. |
| Build sits in the queue | Another OpenSearch build is running, or the agent is offline. | Wait, or ask the Jenkins administrator to check the `onprem-ai-services-machine` agent. |
| Disk or memory errors in **Build** | The agent is low on resources. | The **Build** stage prints `df -h` and `free -h` at the top. Ask the administrator to clean the agent. |

## Jenkins credentials used

You don't need to enter these. They're stored in Jenkins.

| Credential ID | Used for |
| --- | --- |
| `icr-iam-creds` | Pushing the image to ICR. |
