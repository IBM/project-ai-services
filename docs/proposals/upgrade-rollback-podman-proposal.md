# Podman System Upgrade and Rollback Design

## Overview

This document describes the system-wide upgrade and rollback mechanism for AI Services deployed on Podman. The approach upgrades all components together in a coordinated sequence using pod recreation with version-mapped images from `values.yaml` files, providing a simple and reliable upgrade path with minimal downtime.

## Key Concepts

### Version Mapping

- **CLI Version**: Simple version identifier (e.g., `v4`, `v5`, `v6`)
- **Image Versions**: Container image versions stored in `values.yaml` for each runtime
- **Version Mapping**: CLI version (v4, v5) maps to specific image versions in runtime-specific `values.yaml` files

### System Components

The system upgrade includes all deployed components:

1. **Catalog** (Control Plane)
   - UI: `catalog-ui` image
   - Backend: `ai-services` image
   - Database: `postgres` image
   - Proxy: `caddy` image

2. **Components** (Shared Infrastructure)
   - **LLM Providers**: vllm-cpu, vllm-spyre, watsonx
   - **Embedding Providers**: vllm-cpu
   - **Vector DB**: opensearch
   - **Reranker**: vllm-spyre

3. **Services** (Deployed Services)
   - chat, digitize, similarity, summarize

4. **Applications** (Architectures)
   - Multi-service deployments (e.g., RAG)

## Upgrade Strategy: Sequential Pod Restart

### Approach

The upgrade uses **sequential pod recreation** - stopping and removing pods, then recreating them with new image versions in a specific order. This approach:

- ✅ Works with existing single-pod architecture
- ✅ Preserves data (databases use separate pods with PVCs)
- ✅ Simple to implement and understand
- ✅ Minimal downtime per component (15-30 seconds)
- ✅ No template changes required
- ✅ Coordinated system-wide upgrade

### Upgrade Sequence

```
Phase 1: Catalog Upgrade (Control Plane)
    ↓
Phase 2: Component Upgrade (Shared Infrastructure)
    ↓
Phase 3: Service Upgrade (Individual Services)
    ↓
Phase 4: Application Upgrade (Architectures)
```

**Rationale for Sequence:**

1. **Catalog First**: Control plane must be upgraded first to manage other components
2. **Components Second**: Shared infrastructure (LLM, embedding, vector DB) upgraded before services
3. **Services Third**: Individual services upgraded after their dependencies
4. **Applications Last**: Multi-service architectures upgraded after all services

## Version Resolution

### CLI Version to Image Version Mapping

Each CLI release contains embedded `values.yaml` files that correspond to that version. When upgrading to `v5`, the v5 CLI binary contains `values.yaml` files with v5-compatible image versions.

```
ai-services/assets/
├── catalog/podman/values.yaml          # Catalog images
├── services/chat/podman/values.yaml    # Chat service images
├── services/digitize/podman/values.yaml
├── services/similarity/podman/values.yaml
├── services/summarize/podman/values.yaml
├── components/llm/vllm-cpu/podman/values.yaml
├── components/llm/vllm-spyre/podman/values.yaml
├── components/embedding/vllm-cpu/podman/values.yaml
└── components/vector_db/opensearch/podman/values.yaml
```

### Example: Catalog values.yaml (for CLI v5)

```yaml
# ai-services/assets/catalog/podman/values.yaml
# This values.yaml corresponds to CLI version v5
ui:
  image: icr.io/ai-services-cicd/catalog-ui:v0.0.39
backend:
  image: icr.io/ai-services-cicd/ai-services:v0.0.152
db:
  image: icr.io/ai-services-cicd/postgres:18-3
caddy:
  image: icr.io/ai-services-cicd/caddy:v2.11.4-0
```

## System Upgrade Workflow

### Command

```bash
ai-services catalog upgrade --version v5
```

### Step-by-Step Process

#### Phase 1: Pre-Upgrade Preparation

**Step 1: Create Manual Backups**

Users must manually create backups before upgrade:

```bash
# Backup catalog using catalog backup command
ai-services catalog backup --filename backup_catalog.tar.gz

# Backup applications using application backup command
ai-services application backup <app-name> --target <opensearch|digitize>
```

**Step 2: Resolve Image Versions**

```go
// CLI v5 binary resolves all image versions from embedded values.yaml
images := ResolveAllImageVersions("v5", "podman")
// Returns map of all component images for v5
```

#### Phase 2: Catalog Upgrade

**Step 1: Pull Catalog Images**

```bash
podman pull icr.io/ai-services-cicd/catalog-ui:v0.0.39
podman pull icr.io/ai-services-cicd/ai-services:v0.0.152
```

**Step 2: Stop Catalog Pod**

```bash
podman pod stop catalog--catalog
```

**Step 3: Remove Catalog Pod**

```bash
podman pod rm catalog--catalog
```

**Step 4: Update Values and Regenerate YAML**

```bash
# Update catalog values.yaml with v5 images
# Regenerate catalog.yaml from template
```

**Step 5: Recreate Catalog Pod**

```bash
podman play kube catalog.yaml
```

**Step 6: Wait for Catalog Health**

```bash
# Wait for UI and backend to be healthy
# Timeout: 60 seconds
```

**Step 7: Update Database Version**

```sql
UPDATE catalog_metadata SET version = 'v5' WHERE component = 'catalog';
```

**Catalog Upgrade Downtime:** ~15-30 seconds

#### Phase 3: Component Upgrade

**For Each Component (LLM, Embedding, Vector DB, Reranker):**

**Step 1: Query Components from Database**

```go
components := componentRepo.GetAll(ctx)
// Returns all deployed components
```

**Step 2: Pull Component Images**

```bash
# For each component
podman pull <component-image-v5>
```

**Step 3: Stop Component Pod**

```bash
instanceSlug=$(generate_instance_slug <component-id>)
podName="${component.Type}-${component.Provider}-${instanceSlug}"
podman pod stop $podName
```

**Step 4: Remove Component Pod**

```bash
podman pod rm $podName
```

**Step 5: Update Values and Regenerate YAML**

```bash
# Update component values.yaml with v5 images
# Regenerate component YAML from template
```

**Step 6: Recreate Component Pod**

```bash
podman play kube ${component.Type}-${component.Provider}.yaml
```

**Step 7: Wait for Component Health**

```bash
# Wait for component to be healthy
# Timeout: 120 seconds (longer for model loading)
```

**Step 8: Update Database**

```sql
UPDATE components SET version = 'v5', status = 'Running' WHERE id = '<component-id>';
```

**Component Upgrade Downtime:** ~20-60 seconds per component

#### Phase 4: Service Upgrade

**For Each Service (chat, digitize, similarity, summarize):**

**Step 1: Query Services from Database**

```go
services := serviceRepo.GetAll(ctx)
// Returns all deployed services
```

**Step 2: Pull Service Images**

```bash
# For each service
podman pull <service-ui-image-v5>
podman pull <service-backend-image-v5>
```

**Step 3: Stop Service Pod**

```bash
instanceSlug=$(generate_instance_slug <service-id>)
podName="${service.CatalogID}-${instanceSlug}"
podman pod stop $podName
```

**Step 4: Remove Service Pod**

```bash
podman pod rm $podName
```

**Step 5: Update Values and Regenerate YAML**

```bash
# Update service values.yaml with v5 images
# Regenerate service YAML from template
```

**Step 6: Recreate Service Pod**

```bash
podman play kube ${service.CatalogID}.yaml
```

**Step 7: Wait for Service Health**

```bash
# Wait for UI and backend to be healthy
# Timeout: 60 seconds
```

**Step 8: Update Database**

```sql
UPDATE services SET version = 'v5', status = 'Running' WHERE id = '<service-id>';
```

**Service Upgrade Downtime:** ~15-30 seconds per service

#### Phase 5: Application Upgrade

**For Each Application (Architectures):**

**Step 1: Query Applications from Database**

```go
apps := appRepo.GetAll(ctx, &ApplicationFilters{
    DeploymentType: "architectures",
})
// Returns all architecture deployments
```

**Step 2: Upgrade Application Services**

```go
// Each application's services were already upgraded in Phase 4
// Update application version record
```

**Step 3: Update Database**

```sql
UPDATE applications SET version = 'v5' WHERE id = '<app-id>';
```

#### Phase 6: Post-Upgrade Verification

**Step 1: Verify All Pods Running**

```bash
podman pod ps
# Verify all pods are in "Running" state
```

**Step 2: Check Health Status**

```bash
ai-services catalog info --health
# Verify all components report healthy
```

**Step 3: Test Endpoints**

```bash
# Test catalog UI
curl https://localhost:8443/

# Test service endpoints
curl https://localhost:8443/api/v1/services
```

**Step 4: Review Logs**

```bash
# Check for errors in logs
podman logs catalog--catalog--backend
```

**Step 5: Record Upgrade**

```sql
INSERT INTO version_history (component_type, previous_version, current_version, upgraded_at)
VALUES ('system', 'v4', 'v5', NOW());
```

## System Rollback Workflow

### Command

```bash
ai-services catalog rollback --version v4
```

### Prerequisites

1. **Previous CLI Binary**: v4 CLI binary must be available
2. **Image Availability**: v4 images must be available in registry

### Step-by-Step Process

#### Phase 1: Pre-Rollback Preparation

**Step 1: Confirm Rollback**

```bash
# User confirmation required
echo "Rolling back from v5 to v4. This will:"
echo "  - Recreate all pods with v4 images"
echo "  - Estimated downtime: 5-7 minutes"
echo "Note: If data is lost, restore manually using restore commands after rollback"
read -p "Continue? (yes/no): " confirm
```

#### Phase 2: Catalog Rollback

**Step 1: Resolve v4 Image Versions**

```go
// Use v4 CLI binary to get v4 image versions
images := ResolveAllImageVersions("v4", "podman")
```

**Step 2: Pull v4 Catalog Images**

```bash
podman pull icr.io/ai-services-cicd/catalog-ui:v0.0.38
podman pull icr.io/ai-services-cicd/ai-services:v0.0.151
```

**Step 3: Stop and Remove Catalog Pod**

```bash
podman pod stop catalog--catalog
podman pod rm catalog--catalog
```

**Step 4: Update Values with v4 Images**

```bash
# Update catalog values.yaml with v4 images
# Regenerate catalog.yaml from template
```

**Step 5: Recreate Catalog Pod**

```bash
podman play kube catalog.yaml
```

**Step 6: Wait for Catalog Health**

```bash
# Wait for catalog to be healthy
```

#### Phase 4: Component Rollback

**For Each Component:**

**Step 1: Pull v4 Component Images**

```bash
podman pull <component-image-v4>
```

**Step 2: Stop and Remove Component Pod**

```bash
podman pod stop $podName
podman pod rm $podName
```

**Step 3: Recreate with v4 Images**

```bash
podman play kube ${component.Type}-${component.Provider}.yaml
```

**Step 4: Wait for Health**

```bash
# Wait for component to be healthy
```

#### Phase 5: Service Rollback

**For Each Service:**

**Step 1: Pull v4 Service Images**

```bash
podman pull <service-ui-image-v4>
podman pull <service-backend-image-v4>
```

**Step 2: Stop and Remove Service Pod**

```bash
podman pod stop $podName
podman pod rm $podName
```

**Step 3: Recreate with v4 Images**

```bash
podman play kube ${service.CatalogID}.yaml
```

**Step 4: Wait for Health**

```bash
# Wait for service to be healthy
```

#### Phase 6: Application Rollback

**Step 1: Update Application Versions**

```sql
UPDATE applications SET version = 'v4' WHERE version = 'v5';
```

#### Phase 7: Post-Rollback Verification

**Step 1: Verify System State**

```bash
ai-services catalog info --versions
# Should show all components at v4
```

**Step 2: Test Functionality**

```bash
# Test critical endpoints
# Verify data integrity
```

**Step 3: Record Rollback**

```sql
INSERT INTO version_history (component_type, previous_version, current_version, upgraded_at)
VALUES ('system', 'v5', 'v4', NOW());
```

## Data Management

### Backup and Restore

Users are responsible for manually creating backups before upgrade:

**Catalog Backup:**

```bash
ai-services catalog backup --filename backup_catalog.tar.gz
```

**Application Backup:**

```bash
ai-services application backup <app-name> --target <opensearch|digitize>
```

**Restore (if needed after rollback):**

```bash
# Restore catalog
ai-services catalog restore --filename backup_catalog.tar.gz

# Restore application
ai-services application restore <app-name> --target <opensearch|digitize> --filename backup_app.tar.gz
```

## Implementation

### Core System Upgrade Manager

```go
package upgrade

import (
    "context"
    "fmt"
    "os/exec"
    "time"

    "github.com/google/uuid"
    "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
    "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
    "github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type SystemUpgradeManager struct {
    appRepo       repository.ApplicationRepository
    serviceRepo   repository.ServiceRepository
    componentRepo repository.ComponentRepository
    versionRepo   repository.VersionHistoryRepository
    runtime       string
    cliVersion    string
}

func NewSystemUpgradeManager(
    appRepo repository.ApplicationRepository,
    serviceRepo repository.ServiceRepository,
    componentRepo repository.ComponentRepository,
    versionRepo repository.VersionHistoryRepository,
    runtime string,
    cliVersion string,
) *SystemUpgradeManager {
    return &SystemUpgradeManager{
        appRepo:       appRepo,
        serviceRepo:   serviceRepo,
        componentRepo: componentRepo,
        versionRepo:   versionRepo,
        runtime:       runtime,
        cliVersion:    cliVersion,
    }
}

// UpgradeSystem performs a complete system upgrade
func (m *SystemUpgradeManager) UpgradeSystem(ctx context.Context, targetVersion string) error {
    logger.Infof("Starting system upgrade from %s to %s", m.cliVersion, targetVersion)

    // Get current version
    currentVersion, err := m.getCurrentSystemVersion(ctx)
    if err != nil {
        return fmt.Errorf("failed to get current version: %w", err)
    }

    // Create checkpoint for rollback
    checkpoint, err := m.createCheckpoint(ctx)
    if err != nil {
        return fmt.Errorf("failed to create checkpoint: %w", err)
    }

    // Phase 1: Upgrade Catalog
    logger.Info("Phase 1/4: Upgrading Catalog...")
    if err := m.upgradeCatalog(ctx, targetVersion); err != nil {
        logger.Errorf("Catalog upgrade failed: %v", err)
        m.rollbackToCheckpoint(ctx, checkpoint)
        return fmt.Errorf("catalog upgrade failed: %w", err)
    }
    logger.Info("✓ Catalog upgraded successfully")

    // Phase 2: Upgrade Components
    logger.Info("Phase 2/4: Upgrading Components...")
    components, err := m.componentRepo.GetAll(ctx)
    if err != nil {
        return fmt.Errorf("failed to get components: %w", err)
    }

    for i, comp := range components {
        logger.Infof("  Upgrading component %d/%d: %s/%s", i+1, len(components), comp.Type, comp.Provider)
        if err := m.upgradeComponent(ctx, comp, targetVersion); err != nil {
            logger.Errorf("Component upgrade failed: %v", err)
            m.rollbackToCheckpoint(ctx, checkpoint)
            return fmt.Errorf("component upgrade failed: %w", err)
        }
    }
    logger.Infof("✓ All %d components upgraded successfully", len(components))

    // Phase 3: Upgrade Services
    logger.Info("Phase 3/4: Upgrading Services...")
    services, err := m.serviceRepo.GetAll(ctx)
    if err != nil {
        return fmt.Errorf("failed to get services: %w", err)
    }

    for i, svc := range services {
        logger.Infof("  Upgrading service %d/%d: %s", i+1, len(services), svc.CatalogID)
        if err := m.upgradeService(ctx, svc, targetVersion); err != nil {
            logger.Errorf("Service upgrade failed: %v", err)
            m.rollbackToCheckpoint(ctx, checkpoint)
            return fmt.Errorf("service upgrade failed: %w", err)
        }
    }
    logger.Infof("✓ All %d services upgraded successfully", len(services))

    // Phase 4: Upgrade Applications
    logger.Info("Phase 4/4: Upgrading Applications...")
    apps, err := m.appRepo.GetAll(ctx, &repository.ApplicationFilters{
        DeploymentType: "architectures",
    })
    if err != nil {
        return fmt.Errorf("failed to get applications: %w", err)
    }

    for i, app := range apps {
        logger.Infof("  Upgrading application %d/%d: %s", i+1, len(apps), app.Name)
        if err := m.upgradeApplication(ctx, app, targetVersion); err != nil {
            logger.Errorf("Application upgrade failed: %v", err)
            m.rollbackToCheckpoint(ctx, checkpoint)
            return fmt.Errorf("application upgrade failed: %w", err)
        }
    }
    logger.Infof("✓ All %d applications upgraded successfully", len(apps))

    logger.Infof("✅ System upgrade completed successfully: %s → %s", currentVersion, targetVersion)
    return nil
}

// RollbackSystem performs a complete system rollback
func (m *SystemUpgradeManager) RollbackSystem(ctx context.Context, targetVersion string) error {
    logger.Infof("Starting system rollback to version %s", targetVersion)

    currentVersion, err := m.getCurrentSystemVersion(ctx)
    if err != nil {
        return fmt.Errorf("failed to get current version: %w", err)
    }

    // Rollback in reverse order (no automatic database restore)
    logger.Info("Phase 1/4: Rolling back Applications...")
    apps, _ := m.appRepo.GetAll(ctx, &repository.ApplicationFilters{
        DeploymentType: "architectures",
    })
    for _, app := range apps {
        m.rollbackApplication(ctx, app, targetVersion)
    }

    logger.Info("Phase 2/4: Rolling back Services...")
    services, _ := m.serviceRepo.GetAll(ctx)
    for _, svc := range services {
        m.rollbackService(ctx, svc, targetVersion)
    }

    logger.Info("Phase 3/4: Rolling back Components...")
    components, _ := m.componentRepo.GetAll(ctx)
    for _, comp := range components {
        m.rollbackComponent(ctx, comp, targetVersion)
    }

    logger.Info("Phase 4/4: Rolling back Catalog...")
    if err := m.rollbackCatalog(ctx, targetVersion); err != nil {
        return fmt.Errorf("catalog rollback failed: %w", err)
    }

    logger.Infof("✅ System rollback completed successfully: %s → %s", currentVersion, targetVersion)
    logger.Info("Note: If data is lost, restore manually using restore commands")
    return nil
}

// Helper methods

func (m *SystemUpgradeManager) upgradeCatalog(ctx context.Context, version string) error {
    // Implementation for catalog upgrade
    return nil
}

func (m *SystemUpgradeManager) upgradeComponent(ctx context.Context, comp models.Component, version string) error {
    // Implementation for component upgrade
    return nil
}

func (m *SystemUpgradeManager) upgradeService(ctx context.Context, svc models.Service, version string) error {
    // Implementation for service upgrade
    return nil
}

func (m *SystemUpgradeManager) upgradeApplication(ctx context.Context, app models.Application, version string) error {
    // Update application version in database
    return m.appRepo.Update(ctx, app.ID, repository.ApplicationUpdate{
        Version: version,
    })
}

func (m *SystemUpgradeManager) createCheckpoint(ctx context.Context) (*Checkpoint, error) {
    // Create checkpoint for rollback
    return &Checkpoint{}, nil
}

func (m *SystemUpgradeManager) rollbackToCheckpoint(ctx context.Context, checkpoint *Checkpoint) error {
    // Rollback to checkpoint
    return nil
}

func (m *SystemUpgradeManager) getCurrentSystemVersion(ctx context.Context) (string, error) {
    // Get current system version from database
    return m.cliVersion, nil
}

func (m *SystemUpgradeManager) updateSystemVersion(ctx context.Context, version string) error {
    // Update system version in database
    return nil
}

func (m *SystemUpgradeManager) recordUpgrade(ctx context.Context, componentType string,
                                             componentID *uuid.UUID, previousVersion, currentVersion string) error {
    // Record upgrade in version_history table
    return nil
}

func (m *SystemUpgradeManager) restoreDatabases(ctx context.Context, version string) error {
    // Restore all databases from backups
    return nil
}

func (m *SystemUpgradeManager) rollbackCatalog(ctx context.Context, version string) error {
    // Rollback catalog
    return nil
}

func (m *SystemUpgradeManager) rollbackComponent(ctx context.Context, comp models.Component, version string) error {
    // Rollback component
    return nil
}

func (m *SystemUpgradeManager) rollbackService(ctx context.Context, svc models.Service, version string) error {
    // Rollback service
    return nil
}

func (m *SystemUpgradeManager) rollbackApplication(ctx context.Context, app models.Application, version string) error {
    // Rollback application
    return nil
}

type Checkpoint struct {
    Version       string
    Timestamp     time.Time
    BackupPaths   map[string]string
}
```

## CLI Commands

### Upgrade Command

```bash
# System-wide upgrade
ai-services catalog upgrade --version v5

# Dry run (show what would be upgraded)
ai-services catalog upgrade --version v5 --dry-run
```

### Rollback Command

```bash
# System-wide rollback
ai-services catalog rollback --version v4

# Rollback to previous version
ai-services catalog rollback --to-previous

# Dry run
ai-services catalog rollback --version v4 --dry-run
```

## Usage Examples

### Example 1: System Upgrade from v4 to v5

```bash
# Check current version
$ ai-services catalog info --versions
System Version: v4
Catalog: v4
Components: 3 (all v4)
Services: 3 (all v4)
Applications: 1 (v4)

# Perform system upgrade
$ ai-services catalog upgrade --version v5
Starting system upgrade from v4 to v5...

Creating database backups...
  ✓ Catalog database backed up
  ✓ Digitize database backed up
  ✓ Summarize database backed up

Phase 1/4: Upgrading Catalog...
  Pulling images...
  ✓ catalog-ui:v0.0.39
  ✓ ai-services:v0.0.152
  Stopping catalog pod...
  Recreating catalog pod...
  Waiting for health checks...
  ✓ Catalog upgraded (18s downtime)

Phase 2/4: Upgrading Components...
  Upgrading component 1/3: llm/vllm-cpu
    ✓ Upgraded (45s downtime)
  Upgrading component 2/3: embedding/vllm-cpu
    ✓ Upgraded (42s downtime)
  Upgrading component 3/3: vector_db/opensearch
    ✓ Upgraded (25s downtime)
  ✓ All 3 components upgraded

Phase 3/4: Upgrading Services...
  Upgrading service 1/3: chat
    ✓ Upgraded (22s downtime)
  Upgrading service 2/3: digitize
    ✓ Upgraded (28s downtime)
  Upgrading service 3/3: similarity
    ✓ Upgraded (20s downtime)
  ✓ All 3 services upgraded

Phase 4/4: Upgrading Applications...
  Upgrading application 1/1: rag-architecture
    ✓ Upgraded
  ✓ All 1 applications upgraded

✅ System upgrade completed successfully: v4 → v5
Total time: 4m 32s
Total downtime: ~3m 20s (sequential)
```

### Example 2: System Rollback from v5 to v4

```bash
$ ai-services catalog rollback --version v4
Starting system rollback to version v4...

⚠️  WARNING: This will:
  - Restore database backups from v4
  - Recreate all pods with v4 images
  - Any data created after v4 upgrade will be lost
  - Estimated downtime: 5-7 minutes

Continue? (yes/no): yes

Restoring databases...
  ✓ Catalog database restored
  ✓ Digitize database restored
  ✓ Summarize database restored

Phase 1/4: Rolling back Applications...
  ✓ Application versions updated

Phase 2/4: Rolling back Services...
  Rolling back service 1/3: chat
    ✓ Rolled back (20s downtime)
  Rolling back service 2/3: digitize
    ✓ Rolled back (25s downtime)
  Rolling back service 3/3: similarity
    ✓ Rolled back (18s downtime)
  ✓ All services rolled back

Phase 3/4: Rolling back Components...
  Rolling back component 1/3: llm/vllm-cpu
    ✓ Rolled back (40s downtime)
  Rolling back component 2/3: embedding/vllm-cpu
    ✓ Rolled back (38s downtime)
  Rolling back component 3/3: vector_db/opensearch
    ✓ Rolled back (22s downtime)
  ✓ All components rolled back

Phase 4/4: Rolling back Catalog...
  ✓ Catalog rolled back (15s downtime)

✅ System rollback completed successfully: v5 → v4
Total time: 3m 45s
```

### Example 3: Dry Run Upgrade

```bash
$ ai-services catalog upgrade --version v5 --dry-run
Dry run: Showing what would be upgraded to v5

Current System Version: v4

Catalog:
  Current: v4
  Target: v5
  Images:
    - catalog-ui: v0.0.38 → v0.0.39
    - ai-services: v0.0.151 → v0.0.152
  Estimated downtime: 18s

Components (3):
  1. llm/vllm-cpu
     Current: v4 → Target: v5
     Shared by: 2 services
     Estimated downtime: 45s

  2. embedding/vllm-cpu
     Current: v4 → Target: v5
     Shared by: 3 services
     Estimated downtime: 42s

  3. vector_db/opensearch
     Current: v4 → Target: v5
     Shared by: 3 services
     Estimated downtime: 25s

Services (3):
  1. chat-3d2785bfc1
     Current: v4 → Target: v5
     Estimated downtime: 22s

  2. digitize-6fca4cbf99
     Current: v4 → Target: v5
     Estimated downtime: 28s

  3. similarity-6fca4cbf99
     Current: v4 → Target: v5
     Estimated downtime: 20s

Applications (1):
  1. rag-architecture
     Current: v4 → Target: v5
     Services: 3 (already counted above)

Total Estimated Downtime: ~3m 20s (sequential)
Total Estimated Time: ~4-5 minutes

Database Backups Required:
  - catalog (ai_services)
  - digitize (digitize)
  - summarize (summarize)
```

## Error Handling

### Automatic Rollback on Failure

```go
func (m *SystemUpgradeManager) UpgradeSystem(ctx context.Context, targetVersion string) error {
    // Create checkpoint before upgrade
    checkpoint, err := m.createCheckpoint(ctx)
    if err != nil {
        return fmt.Errorf("failed to create checkpoint: %w", err)
    }

    // Attempt upgrade
    err = m.performUpgrade(ctx, targetVersion)
    if err != nil {
        logger.Errorf("Upgrade failed: %v", err)
        logger.Info("Attempting automatic rollback...")

        if rollbackErr := m.rollbackToCheckpoint(ctx, checkpoint); rollbackErr != nil {
            logger.Errorf("Automatic rollback failed: %v", rollbackErr)
            return fmt.Errorf("upgrade failed and rollback failed: %w, rollback error: %v", err, rollbackErr)
        }

        logger.Info("✅ Automatic rollback successful")
        return fmt.Errorf("upgrade failed but rolled back successfully: %w", err)
    }

    return nil
}
```

### Health Check Failures

If health checks fail during upgrade:

1. Wait for timeout (60-120 seconds)
2. If still unhealthy, trigger automatic rollback
3. Restore from checkpoint
4. Report failure with detailed logs

## Best Practices

### Pre-Upgrade Checklist

- [ ] Review release notes for v5
- [ ] Create manual backups using backup commands
- [ ] Check image availability in registry
- [ ] Test upgrade in non-production environment
- [ ] Schedule maintenance window
- [ ] Notify users of planned downtime
- [ ] Ensure previous CLI version (v4) is available for rollback

### Post-Upgrade Verification

- [ ] Verify all pods are running
- [ ] Test catalog UI access
- [ ] Test service endpoints
- [ ] Review logs for errors
- [ ] Check resource usage
- [ ] Test critical user workflows
- [ ] Update documentation

### Rollback Decision Criteria

Rollback immediately if:

- Catalog fails to start after 2 minutes
- More than 50% of components fail health checks
- Critical functionality is broken
- Resource usage exceeds 90% of available

### Post-Rollback Data Restore

If data is lost after rollback, manually restore using:

```bash
# Restore catalog
ai-services catalog restore --filename backup_catalog.tar.gz

# Restore applications
ai-services application restore <app-name> --target <opensearch|digitize> --filename backup_app.tar.gz
```

## Monitoring and Logging

### Upgrade Progress Tracking

```go
type UpgradeProgress struct {
    Phase              string
    CurrentComponent   string
    ComponentsTotal    int
    ComponentsComplete int
    StartTime          time.Time
    EstimatedRemaining time.Duration
}
```

### Logging

```bash
# Upgrade logs
[INFO] Starting system upgrade: v4 → v5
[INFO] Phase 1/4: Upgrading Catalog
[INFO]   Pulling image: catalog-ui:v0.0.39
[INFO]   Pod stopped: catalog--catalog
[INFO]   Pod created: catalog--catalog
[INFO]   Health check passed: catalog--catalog
[INFO] ✓ Catalog upgraded (18s)
[INFO] Phase 2/4: Upgrading Components
[INFO]   Upgrading component 1/3: llm/vllm-cpu
[INFO]   Health check passed: llm-vllm-cpu-3d2785bfc1
[INFO] ✓ Component upgraded (45s)
[INFO] ✅ System upgrade completed: v4 → v5
```

## Summary

This design provides a simplified coordinated system-wide upgrade and rollback mechanism:

- **System-Wide Only**: All components upgraded together in coordinated sequence
- **Simple**: Uses pod recreation, no complex orchestration
- **Reliable**: Preserves data, enables rollback
- **Sequential**: Upgrades in order: Catalog → Components → Services → Applications
- **Minimal Downtime**: 15-30 seconds per component, ~3-5 minutes total
- **Version-Driven**: CLI version (v4, v5) maps to image versions via embedded values.yaml
- **Manual Backups**: Users create backups using catalog backup and application backup commands
- **Manual Restore**: If data is lost after rollback, users restore using restore commands
- **Rollback-Ready**: Automatic rollback on failure, manual rollback supported
- **Production-Ready**: Error handling, health checks, progress tracking

The approach works with existing templates and requires no architectural changes, making it ideal for production deployments.
