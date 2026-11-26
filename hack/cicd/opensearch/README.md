# OpenSearch (ppc64le) Image Pipeline

The [`Jenkinsfile`](./Jenkinsfile) in this directory builds OpenSearch from source for `ppc64le`, packages it as a container image, validates and scans it, and pushes it to IBM Container Registry (ICR).

## Prerequisites

- Access to the [Jenkins server](https://sys-powercloud-team-jenkins.swg-devops.com/) and permission to run the OpenSearch job (`ai-services` folder).
- A version in the form `X.Y.Z` (for example `3.8.0`, `3.10.1`) that is a **git tag** in [opensearch-build](https://github.com/opensearch-project/opensearch-build/tags).
- The matching `X.Y.0` files in [ppc64le/build-scripts](https://github.com/ppc64le/build-scripts) (see below).

## Files used per version

Dockerfiles and patches always come from the `X.Y.0` folder, so a patch release such as `3.10.1` reuses the `3.10.0` files. Folders are under `o/opensearch-project-opensearch-build/Dockerfiles/<X.Y.0>_ubi9.7/` in build-scripts.

| Version | Patches applied, in order | `DEPENDENCY_UPDATES` |
| --- | --- | --- |
| 3.8.x | `ppc64le-<X.Y.0>-ai-services.patch` | Not supported |
| 3.10.x and above | `ppc64le-<X.Y.0>-ai-services.patch`, `ppc64le-<X.Y.0>-dependency-patches.patch` | Supported |

- `ai-services.patch`: the `ppc64le` fixes (component build scripts, `opensearch-build` Python code). For 3.8 it also carries the dependency bumps.
- `dependency-patches.patch`: a hook in `builder_from_source.py` that applies the patches generated from `DEPENDENCY_UPDATES` after each component checkout. It does nothing when no updates are set.

### Generate a patch

1. Clone `opensearch-build` at the version tag and create a branch:

   ```bash
   git clone --branch <version> https://github.com/opensearch-project/opensearch-build.git
   cd opensearch-build && git checkout -b ppc64le-<version>
   ```

2. Apply the previous release's patch and fix the hunks that fail:

   ```bash
   git apply --3way <build-scripts>/o/opensearch-project-opensearch-build/Dockerfiles/<previous X.Y.0>_ubi9.7/ppc64le-<previous X.Y.0>-ai-services.patch
   ```

3. Make any further `ppc64le` changes and confirm the build works.
4. Export the patch and check it applies to a clean checkout of the tag:

   ```bash
   git add -A
   git diff --cached --binary <version> > ppc64le-<X.Y.0>-ai-services.patch
   git -C <clean-checkout> apply --check ppc64le-<X.Y.0>-ai-services.patch
   ```

5. Copy it to the `X.Y.0` folder in build-scripts. For 3.10 and above, also add the dependency hook patch (see `scripts/README.md` in build-scripts).

A patch release (`X.Y.Z`) needs a new patch only if the `X.Y.0` patch no longer applies to its tag (`git apply` error in **Build**).

### Test a patch from a fork

Push the patch to a branch in your build-scripts fork and run the pipeline with `BUILD_SCRIPTS_REPO` and `BUILD_SCRIPTS_BRANCH` pointing to it.

## Run the pipeline

1. In Jenkins, open the OpenSearch job and click **Build with Parameters**.
2. Set the parameters below and click **Build**.
3. Watch progress under **Build History** > build number > **Console Output**.

| Parameter | Default | Description |
| --- | --- | --- |
| `OPENSEARCH_VERSION` | `3.8.0` | Version to build, `X.Y.Z`. Must be a tag in `opensearch-build`. |
| `DEPENDENCY_UPDATES` | empty | 3.10 and above only. `<target>:<key>=<version>` entries separated by comma, space or newline. `<target>` is `opensearch`, `security` or `opensaml`. Example: `security:guava=33.7.3-jre,opensaml:open_saml=5.2.4`. The build fails if set for an older version. |
| `BUILD_SCRIPTS_REPO` | `https://github.com/ppc64le/build-scripts.git` | HTTPS GitHub URL of the build-scripts repo. Change only to test a fork. |
| `BUILD_SCRIPTS_BRANCH` | `master` | Branch of that repo. |

Every successful build pushes an image, including builds from a fork. The build number in the tag tells them apart.

## Stages

The pipeline runs on the `onprem-ai-services-machine` agent, one build at a time.

| Stage | What happens | Fails the build? |
| --- | --- | --- |
| **Prepare** | Validates parameters. Checks out this repo, `opensearch-build` (version tag) and `build-scripts`. | Yes |
| **Build** | Builds the build-env image, applies the patches, builds OpenSearch and its plugins in Podman, assembles the tarball. For 3.10 and above, also generates the dependency patches. | Yes |
| **Build Container Image** | Builds the container image from the tarball. | Yes |
| **Validate** | Smoke-tests the image (cluster health, version), then runs [`test/opensearch/validate.sh`](../../../test/opensearch/validate.sh). | No. A failure marks the stage unstable and the push still happens. |
| **Security Scan** | Trivy scan for CRITICAL and HIGH vulnerabilities (unfixed ignored). | Yes, if the scan can't run |
| **Push to ICR** | Pushes the image. | Yes |

Plugins built: `common-utils`, `opensearch-remote-metadata-sdk`, `job-scheduler`, `security`, `custom-codecs`, `k-NN`, `ml-commons`, `neural-search`, `opensearch-observability`, `index-management`.

## Output

- Image: `icr.io/ai-services-cicd/opensearch:<version>-ppc64le-<build number>`
- Build artifacts: `trivy-report/trivy-results.{txt,json}`, `opensearch-build/tar/dist/opensearch/*` (also copied to `/var/jenkins/artifacts/opensearch/` on the agent), and `dependency-patches/*` (3.10 and above with `DEPENDENCY_UPDATES` set).

To check a build: success is green or blue, unstable is yellow, and failed is red. The last console line reads `OpenSearch <version> (ppc64le) image successfully published to ...`. For a yellow build, read the **Validate** log. For all builds, review `trivy-results.txt`.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| **Prepare**: `OPENSEARCH_VERSION must be in the form X.Y.Z` | Use `X.Y.Z`, for example `3.8.0` (not `v3.8.0` or `3.8`). |
| **Prepare**: `DEPENDENCY_UPDATES is supported for OpenSearch 3.10 and above only` | Clear `DEPENDENCY_UPDATES` or build 3.10 or above. |
| **Prepare**: `couldn't find remote ref refs/tags/<version>` | The version isn't a tag in `opensearch-build`. Check the spelling. |
| **Prepare**: `BUILD_SCRIPTS_REPO must be an HTTPS GitHub URL` | Use `https://github.com/<user>/<repo>.git`. |
| **Prepare**: checkout of `build-scripts` fails | The branch doesn't exist in `BUILD_SCRIPTS_REPO`, or the repo is private. |
| **Build**: `No such file or directory` for a Dockerfile or patch | The `X.Y.0` folder or a patch is missing in build-scripts. Add it and rebuild. |
| **Build**: `git apply` errors | The patch doesn't apply to this tag. Regenerate it. |
| **Validate** is yellow | Smoke test or `validate.sh` failed. The image was still pushed. |
| **Push to ICR**: authentication error | ICR credentials in Jenkins are expired or missing. Ask the Jenkins administrator. |
| Build stays queued | Another build is running or the agent is offline. |
| Disk or memory errors in **Build** | The stage prints `df -h` and `free -h` at the top. Ask the administrator to clean the agent. |
