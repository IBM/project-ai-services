# Implementation Plan: Repository Reorganization

## Overview
This plan divides the reorganization into **2 parallel work streams** that can be executed by separate developers with minimal blocking dependencies. The key is to separate **backend services** from **frontend UI** work, as they have different technology stacks and minimal cross-dependencies.

---

## Work Stream 1: Backend Services & Infrastructure
**Developer A** - Focus: Python services, container builds, base images

### Phase 1: Foundation Setup (Days 1-2)
**Goal:** Create new directory structure and base image layer

#### Tasks
1. **Create new directory structure**
   ```bash
   mkdir -p services/{common,chatbot,digitize,summarize,similarity}
   mkdir -p images/python-base
   ```

2. **Build base image layer**
   - Use git to move: `git mv images/rag-base images/python-base`
   - Update `images/python-base/Containerfile` to use new naming
   - Update `images/python-base/requirements.txt` if needed
   - Test build: `cd images/python-base && podman build -t python-base:latest .`

3. **Create common library layer**
   - Use git to move: `git mv spyre-rag/src/common services/common`
   - Create `services/common/Containerfile` (extends python-base)
   - Create `services/common/requirements.txt` (common-specific deps only)
   - Test build: `cd services/common && podman build -t services-common:latest .`

#### Deliverable
- Base images built and tested, foundation for all services ready

---

### Phase 2: Service Migration - Chatbot (Days 3-4)
**Goal:** Migrate and containerize chatbot service

#### Tasks
1. **Move chatbot service**
   - Use git to move: `git mv spyre-rag/src/chatbot services/chatbot`
   - Use git to move: `git mv spyre-rag/src/settings.json services/chatbot/settings.json`
   - Create `services/chatbot/Containerfile` (extends services-common)
   - Create `services/chatbot/requirements.txt` (chatbot-specific deps)

2. **Update imports and paths**
   - Verify Python imports still work: `from common.db_utils import ...`
   - Update any hardcoded paths in chatbot code
   - Update entrypoint in Containerfile

3. **Test chatbot service**
   - Build: `cd services/chatbot && podman build -t chatbot-service:latest .`
   - Run standalone test with mock dependencies
   - Verify API endpoints respond

#### Deliverable
- Chatbot service containerized and independently deployable

---

### Phase 3: Service Migration - Digitize, Summarize & Similarity (Days 5-6)
**Goal:** Migrate remaining backend services

#### Tasks
1. **Move digitize service**
   - Use git to move: `git mv spyre-rag/src/digitize services/digitize`
   - Create `services/digitize/Containerfile` (extends services-common)
   - Create `services/digitize/requirements.txt`
   - Build and test: `cd services/digitize && podman build -t digitize-service:latest .`

2. **Move summarize service**
   - Use git to move: `git mv spyre-rag/src/summarize services/summarize`
   - Create `services/summarize/Containerfile` (extends services-common)
   - Create `services/summarize/requirements.txt`
   - Build and test: `cd services/summarize && podman build -t summarize-service:latest .`

3. **Move similarity service**
   - Use git to move: `git mv spyre-rag/src/similarity services/similarity`
   - Create `services/similarity/Containerfile` (extends services-common)
   - Create `services/similarity/requirements.txt`
   - Build and test: `cd services/similarity && podman build -t similarity-service:latest .`

4. **Create service Makefiles**
   - Add `services/chatbot/Makefile` with build/test targets
   - Add `services/digitize/Makefile` with build/test targets
   - Add `services/summarize/Makefile` with build/test targets
   - Add `services/similarity/Makefile` with build/test targets

#### Deliverable
- All backend services migrated and containerized

---

### Phase 4: Deployment Updates (Days 7-8)
**Goal:** Update deployment templates and CI/CD

#### Tasks
1. **Update OpenShift templates**
   - Update `ai-services/assets/applications/rag/openshift/templates/`:
     - `backend-deployment.yaml` - change `chatbot.app:app` to `app:app`
     - `digitize-api-deployment.yaml` - change `digitize.app:app` to `app:app`
     - `similarity-api-deployment.yaml` - change `similarity.app:app` to `app:app`
     - `summarize-api-deployment.yaml` - change `summarize.app:app` to `app:app`
   - Repeat for `rag-dev/` and `rag-cpu/` directories

2. **Update Podman templates**
   - Update `ai-services/assets/applications/rag/podman/templates/`:
     - `chat-bot.yaml.tmpl` - change `chatbot.app:app` to `app:app`
     - `digitize.yaml.tmpl` - change `digitize.app:app` to `app:app`
     - `similarity-api.yaml.tmpl` - change `similarity.app:app` to `app:app`
     - `summarize-api.yaml.tmpl` - change `summarize.app:app` to `app:app`
   - Repeat for `rag-dev/`, `rag-cpu/`, and `services/` subdirectories

3. **Update build scripts**
   - Update `hack/` scripts if they reference old paths
   - Create new build order script for layered builds

4. **Update CI workflows**
   - Rename `spyre-rag-image.yml` to `services-image.yml`
   - Update trigger paths from `spyre-rag/src/**` to `services/**`
   - Update build steps for per-service images

#### Deliverable
- Deployment templates updated, services deployable

---

## Work Stream 2: Frontend UI Applications
**Developer B** - Focus: React/Node.js UIs, nginx configs

### Phase 1: UI Directory Setup (Days 1-2)
**Goal:** Create UI structure and move chatbot UI

#### Tasks
1. **Create new directory structure**
   ```bash
   mkdir -p ui/{chatbot,digitize,catalog}
   ```

2. **Move chatbot UI**
   - Use git to move: `git mv spyre-rag/ui ui/chatbot`
   - Verify `ui/chatbot/Containerfile` is present
   - Verify `ui/chatbot/package.json` is present
   - Update any hardcoded paths in source files

3. **Test chatbot UI build**
   - `cd ui/chatbot && npm install`
   - `npm run build`
   - Build container: `podman build -t chatbot-ui:latest .`
   - Test nginx config with sample backend URL

#### Deliverable
- Chatbot UI migrated and containerized

---

### Phase 2: Digitize UI Migration (Days 3-4)
**Goal:** Move and update digitize UI

#### Tasks
1. **Move digitize UI**
   - Use git to move: `git mv digitize-ui ui/digitize`
   - Verify all assets moved (Containerfile, package.json, nginx.conf.tmpl, src/)
   - Update any references to old paths

2. **Update configuration**
   - Review `ui/digitize/nginx.conf.tmpl` for backend API paths
   - Update environment variable references if needed
   - Check for any hardcoded URLs that need updating

3. **Test digitize UI build**
   - `cd ui/digitize && npm install`
   - `npm run build`
   - Build container: `podman build -t digitize-ui:latest .`
   - Verify static assets serve correctly

#### Deliverable
- Digitize UI migrated and containerized

---

### Phase 3: Catalog UI Migration (Days 5-6)
**Goal:** Move and update catalog UI

#### Tasks
1. **Move catalog UI**
   - Use git to move: `git mv catalog-ui ui/catalog`
   - Verify all assets moved
   - Update any references to old paths

2. **Update configuration**
   - Review `ui/catalog/nginx.conf.tmpl`
   - Update API endpoint configurations
   - Check environment variable usage

3. **Test catalog UI build**
   - `cd ui/catalog && npm install`
   - `npm run build`
   - Build container: `podman build -t catalog-ui:latest .`

#### Deliverable
- Catalog UI migrated and containerized

---

### Phase 4: UI Deployment Updates (Days 7-8)
**Goal:** Update UI deployment templates

#### Tasks
1. **Update OpenShift UI templates**
   - Update `ai-services/assets/applications/rag/openshift/templates/`:
     - `ui-deployment.yaml` - update image and paths
     - `digitize-ui-deployment.yaml` - update paths
     - `ui-route.yaml` - verify routing
     - `digitize-ui-route.yaml` - verify routing

2. **Update Podman UI templates**
   - Templates are typically minimal for UIs, verify they reference correct images

3. **Update documentation**
   - Update `docs/INSTALLATION.md` with new paths
   - Update any UI-specific documentation

#### Deliverable
- UI deployment templates updated, UIs deployable

---

## Integration & Cleanup Phase (Days 9-10)
**Both Developers** - Coordinate final integration

### Day 9: Integration Testing

#### Tasks
1. **Build all images in correct order**
   ```bash
   # Base layer
   cd images/python-base && podman build -t python-base:latest .

   # Common layer
   cd services/common && podman build -t services-common:latest .

   # Services (can be parallel)
   cd services/chatbot && podman build -t chatbot-service:latest .
   cd services/digitize && podman build -t digitize-service:latest .
   cd services/summarize && podman build -t summarize-service:latest .
   cd services/similarity && podman build -t similarity-service:latest .

   # UIs (can be parallel)
   cd ui/chatbot && podman build -t chatbot-ui:latest .
   cd ui/digitize && podman build -t digitize-ui:latest .
   cd ui/catalog && podman build -t catalog-ui:latest .
   ```

2. **End-to-end testing**
   - Deploy full RAG stack using updated templates
   - Test chatbot UI -> chatbot service -> OpenSearch flow
   - Test digitize UI -> digitize service -> document ingestion
   - Verify all services communicate correctly

3. **Performance validation**
   - Verify layer caching works (rebuild times)
   - Check image sizes are reasonable
   - Test startup times

---

### Day 10: Cleanup & Documentation

#### Tasks
1. **Remove old directories**
   ```bash
   # spyre-rag/src/ should be empty after all git mv operations
   # spyre-rag/ui/ will be empty after Dev 2's git mv
   git rm -r spyre-rag/
   # digitize-ui/ and catalog-ui/ already gone from Dev 2's git mv
   ```

2. **Update root-level files**
   - Update `README.md` with new structure
   - Update `CONTRIBUTING.md` if it references old paths
   - Update `.gitignore` if needed

3. **Update CI/CD pipelines**
   - Confirm all GitHub Actions workflows reference new paths
   - Verify build order in CI reflects dependency chain
   - Add parallel build jobs where possible

4. **Create migration documentation**
   - Document the new structure in `docs/`
   - Create developer guide for new service structure
   - Document build order and dependencies

---

## Dependency Management

### Critical Dependencies
- **Stream 1 -> Stream 2:** None (can work fully in parallel)
- **Stream 2 -> Stream 1:** None (can work fully in parallel)

### Coordination Points
1. **Day 9:** Both streams must complete before integration testing
2. **Day 10:** Coordinate on final cleanup and documentation

### Risk Mitigation
- Each stream works in new directories, avoiding conflicts
- Using `git mv` preserves file history and makes merging easier
- Git tracks file moves, so concurrent edits in other branches will merge cleanly
- Each phase has independent testing before integration
- Feature branch isolates all changes until integration is validated

---

## Success Criteria

### Stream 1 (Backend)
- All services build successfully with layered approach
- Services run independently with correct imports
- Deployment templates updated and tested
- Build time improved via layer caching

### Stream 2 (Frontend)
- All UIs build and serve static assets
- nginx configs route correctly to backend services
- UI deployment templates updated
- No broken links or missing assets

### Integration
- Full RAG stack deploys successfully
- End-to-end workflows function correctly
- Old directories safely removed
- Documentation updated

---

## Timeline Summary

| Phase | Stream 1 (Backend) | Stream 2 (Frontend) | Dependencies |
|-------|-------------------|---------------------|--------------|
| Days 1-2 | Foundation Setup | UI Directory Setup | None |
| Days 3-4 | Chatbot Migration | Digitize UI Migration | None |
| Days 5-6 | Digitize/Summarize/Similarity Migration | Catalog UI Migration | None |
| Days 7-8 | Deployment Updates | UI Deployment Updates | None |
| Day 9 | Integration Testing | Integration Testing | Both streams complete |
| Day 10 | Cleanup & Documentation | Cleanup & Documentation | Integration complete |

**Total Duration:** 10 working days with 2 developers working in parallel

---

## Key Benefits

1. **Zero blocking dependencies** - Streams work in completely separate directories and technology stacks
2. **Parallel execution** - Both developers can work simultaneously for 8 days
3. **Git history preserved** - Using `git mv` maintains file history and enables clean merges with concurrent work
4. **Safe merging** - Git tracks moves, so changes in other branches merge cleanly
5. **Independent testing** - Each phase includes validation before moving forward
6. **Clear ownership** - Backend vs Frontend separation is natural and intuitive
7. **Minimal coordination** - Only 2 days require synchronization between developers
