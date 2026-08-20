# Proposal: Let CI Handle Image Version Bumps

**Status:** In Review

## The problem

Right now, shipping a code change to any of our services means doing version bookkeeping by hand. Say you fix a bug in the chatbot service. Before your PR can merge, you have to:

1. Bump `TAG?=v0.0.25` to `v0.0.26` in `services/chatbot/Makefile`
2. Find every `values.yaml` under `ai-services/assets/` that references `chatbot-service` and update the tag there too (for chatbot-service that's 7 different files)
3. Bump the `ai-services` image tag as well, since the binary embeds those values files

Then a teammate's PR merges before yours, and their bump conflicts with your bump, and you rebase and renumber. With several of us working at once, this happens constantly. Some PRs end up getting re-bumped two or three times before they land. None of this work has anything to do with the actual change being made.

There are also a couple of quieter problems with the current setup:

- The CI check that verifies values files match the Makefiles (`check_image_names.py`) works off a hardcoded list of file/key pairs. That list is out of date. Around nine values files with pinned image versions aren't checked at all (all of `catalog/openshift`, several of the single-service charts, the watsonx litellm files, and a few individual keys elsewhere). If one of those drifts to a stale version, nothing catches it.
- The translate and extract services have no image build workflows in CI at all. When their code changes, no image gets built or published unless someone does it by hand. They're also missing from the image scanner matrix.

## What we'd change

The short version: **versions move out of PRs entirely.** You merge code, CI figures out what changed, builds and publishes the image with the next version number, and commits the bump itself. Nobody edits a version string by hand for normal day-to-day work.

There are four pieces to this.

### 1. One file that owns all the versions

A new file, `ai-services/assets/versions.yaml`, lists every image we ship and its current tag:

```yaml
registry: icr.io/ai-services-cicd
images:
  chatbot-service:
    tag: v0.0.25
    source: [services/chatbot, services/common]
  chatbot-ui:
    tag: v0.0.50
    source: [ui/chatbot]
  digitize-service:
    tag: v0.0.43
    source: [services/digitize, services/common]
  # ... and so on for all 15 of our images, plus third-party
  # images (opensearch, vllm) which are listed but never auto-bumped
```

The `source` paths tell CI which image a changed file belongs to. The image lines in all 26 values.yaml files become generated output: a small sync tool reads `versions.yaml` and rewrites the tag on every matching `image:` line. Want to know what version of anything we're currently shipping? It's one file, and `git log` on that file is a complete release history.

### 2. CI bumps versions after merge, not you before it

Today the flow looks like this:

> write code → bump Makefile → edit 5–10 values files → open PR → teammate merges first → rebase and re-bump → merge

With this proposal:

> write code → open PR → merge

That's it. After your PR merges, a workflow on `main`:

1. Looks at what changed and maps it to images via the `source` paths (your chatbot fix → `chatbot-service`)
2. Computes the next tag (`v0.0.25` → `v0.0.26`), builds the image, and pushes it to the registry
3. Commits the one-line bump to `versions.yaml` plus the regenerated values files back to `main` as a bot commit, something like `chore(versions): bump chatbot-service to v0.0.26`

Version bumps can't conflict between PRs anymore because PRs don't contain them. If two merges land close together, the bump workflow runs serially and each run picks up whatever hasn't been bumped yet, so nothing gets lost.

Tags stay exactly the shape they are now (`v0.0.26`), just incremented by a machine instead of a person.

### 3. A consistency check that can't have blind spots

The two Python gate scripts go away, replaced by one much simpler PR check: regenerate the values files from `versions.yaml` and fail if anything differs. Because it works by matching image names rather than a hand-maintained list, it covers every values file automatically, including the nine that are unchecked today and any new ones we add later. It also flags any first-party-looking image it doesn't recognize, so the manifest can never silently fall out of date.

### 4. Local development gets simpler, not harder

This was a hard requirement for the design: you need to be able to build and run a changed service *before* anything is committed or merged. The CLI already supports this cleanly. `ai-services application create` accepts `--values` override files that layer on top of the built-in defaults.

So the dev loop becomes:

```bash
# build your work-in-progress image with a throwaway tag
cd services && make dev-image SERVICE=chatbot
# prints: localhost/chatbot-service:dev-a1b2c3d
```

```yaml
# dev-values.yaml (never committed)
backend:
  image: localhost/chatbot-service:dev-a1b2c3d
```

```bash
ai-services application create rag --values dev-values.yaml
```

No tracked files get touched while you iterate. The Makefile `TAG` default changes to `dev-<git sha>` so a local build can never accidentally produce a release-looking tag.

## What doesn't change

- **Tags stay human-readable semver.** `v0.0.26`
- **The release process is untouched.** Jenkins code/image signing, cosign verification, and `hack/promote-images.sh` all keep working exactly as documented in the Release Guide, with the same tag formats. If anything, release prep gets easier: `versions.yaml` *is* the list of images and tags to sign.
- **The values files keep their current shape.** Same keys, same fused `image:` strings. The Go deployer, the Helm charts, and the podman templates need zero changes.
- **Third-party images are left alone.** opensearch, vllm, and friends are listed in `versions.yaml` for visibility (so there's still one place to update them), but CI never touches their tags.

## A few details worth knowing

**The ai-services image keeps its cascade.** Because the binary embeds the values files, today's checks force an `ai-services` bump whenever values change (which is why it's at v0.0.240). The automation keeps that behavior: every bump run also rebuilds `ai-services` so the shipped binary always carries current tags.

**Changes to `services/common` will now rebuild all six Python services.** Today a change to the shared code rebuilds nothing, which is a real gap: services silently run stale copies of common until their next unrelated bump. Fair warning that a common change means six image builds, but that's the correct behavior.

**litellm and caddy upstream upgrades stay a deliberate manual step.** Their tags combine an upstream version with our revision (`v1.89.3-1`). Automatic bumps only increment our revision suffix. Moving to a new upstream version is an intentional one-line edit to `versions.yaml` (plus the Makefile version variable), and the check verifies the two agree.

**Release branches get their own tag lineage.** Both `main` and `release-*` branches publish to the same registry, and each branch carries its own snapshot of `versions.yaml`, so without special handling they'd eventually hand out the same tag to two different builds. Concretely: we cut `release-0.2` while chatbot-service is at `v0.0.26`, main keeps moving and publishes `v0.0.27`, then a hotfix lands on the release branch and its CI (still reading `v0.0.26` from its own `versions.yaml`) also computes `v0.0.27`. Registry tags are just mutable pointers, so whichever push comes second silently overwrites the first, and "v0.0.27" now means different code depending on when you pulled it. That's a problem anywhere, but especially for us since we sign images by tag.

To keep the sequences from ever meeting, the first bump on a release branch appends the branch name and starts a counter (`v0.0.26` → `v0.0.26-release-0.2.1`), and later bumps on that branch just increment the counter (`.2`, `.3`, ...). Main never produces suffixed tags, so collisions are impossible, and the tag becomes self-describing: `v0.0.26-release-0.2.3` is the third fix on `release-0.2`, which forked while chatbot-service was at `v0.0.26`. Signing and promotion don't care about the format, though we should double-check nothing in the Jenkins pipeline assumes tags look exactly like `vX.Y.Z` before the first release-branch bump.

**The bot needs permission to push to main.** The plan is a machine-user account with a fine-grained PAT (contents: write, this repo only) added to the branch protection bypass list.

## How we'd roll it out

Four steps, each its own PR, each safe on its own:

1. **Close the existing gaps.** Add the missing translate and extract image build workflows and scanner entries, copying the pattern the other services already use.
2. **Add the new pieces alongside the old.** `versions.yaml`, the sync tool (Go, with unit tests, living in the `ai-services` module), and the new consistency check. The old gate scripts keep running; nothing about the workflow changes yet.
3. **Dark-launch the bump workflow.** It exists but only runs manually with a dry-run flag, so we can watch it detect changes and print what it *would* build and commit before trusting it.
4. **Cut over.** Turn on the post-merge trigger, delete the two Python gate scripts and the per-service build workflows the orchestrator replaces, and switch the Makefile TAG defaults to dev tags.

One heads-up for step 4: any in-flight PRs that carry old-style manual bumps will need to drop them (the new check will flag the inconsistency). We'd announce the cutover ahead of time so nobody gets surprised mid-review.

## What's next

- Feedback on the approach, especially from anyone who touches the release process
- A thumbs-up to start on step 1, which is a pure gap fix we arguably want regardless

Happy to talk through any of this. The goal is simple: no more manual management of image versions.
