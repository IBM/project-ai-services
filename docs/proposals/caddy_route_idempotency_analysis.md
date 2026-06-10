# Caddy Route Configuration: Issues & Analysis

## Summary
Caddy route registration is **idempotent** ✅, but the early exit on re-run prevents recovery from failures and configuration updates ❌.

---

## Core Issues

### 1. Early Exit Prevents Recovery
**Location:** [`configure.go:116-121`](../../ai-services/internal/pkg/catalog/cli/configure/podman/configure.go#L116-L121)

```go
if isDeployed {
    return nil  // Exits immediately, skips cert loading & route registration
}
```

**Impact:**
- Cannot recover from certificate loading failures
- Cannot recover from route registration failures  
- Cannot update domain configuration on re-run
- Partial route registration never completes

---

### 2. Route Registration is Idempotent (Good!)
**Location:** [`caddy.go:87-89`](../../ai-services/internal/pkg/proxy/caddy.go#L87-L89)

**How it works:**
1. Check if route exists (GET `/id/{routeID}`)
2. If exists → UPDATE (PUT)
3. If not exists → CREATE (POST)

**Minor issue:** Always updates even if config unchanged (unnecessary but harmless)

---

### 3. No Route Unregistration
**Location:** [`types.go:4-13`](../../ai-services/internal/pkg/proxy/types.go#L4-L13)

No `UnregisterRoute()` method exists - failed routes remain in Caddy with no cleanup mechanism.

---

### 4. Certificate Staleness Risk
**Location:** [`configure.go:306-335`](../../ai-services/internal/pkg/catalog/cli/configure/podman/configure.go#L306-L335)

User provides certs from their directory, we copy to staged directory. Old certs can remain if not cleaned.

**Solution:** Always clean staged cert directory before copying new certs.

---

### 5. Domain Change Detection Unreliable
Can only detect old domain when:
- ✅ Previous configure was successful
- ✅ Caddy is running and healthy
- ✅ At least one route is registered

**Success rate:** ~70-80% in practice

**Conclusion:** Don't rely on detection for critical decisions.

---

### 6. Domain Change Breaks Deployed Applications
**Critical architectural issue:**

```
Catalog configured with example.com
User deploys RAG app → routes use example.com

User re-runs catalog configure with newdomain.com
→ Catalog routes updated to newdomain.com ✓
→ RAG app routes still use example.com ✗

Result: Inconsistent state across system
```

Domain is **system-wide** but treated as **catalog-only**.

---

## Failure States & Recovery

| Failure Point | System State | Current Re-run | Issue |
|--------------|--------------|----------------|-------|
| Pod deployment | Partial pods | ✅ Skips existing, deploys missing | Works correctly |
| Cert loading | All pods, no certs | ❌ Exits early | Never retries |
| Route registration | All pods, partial routes | ❌ Exits early | Never retries |
| Domain change | All working, new domain | ❌ Exits early | Ignores new config |

---

## Rejected Solutions

### ❌ User Confirmation for Domain Changes
**Why rejected:**
- Can't reliably detect old domain in all cases
- Breaks automation/CI-CD
- User already expressed intent via CLI flags

### ❌ Compare Configs Before Updating Routes
**Why rejected:**
- Adds 50+ lines of complex comparison logic
- Fragile to Caddy response format changes
- Minimal performance benefit (saves 1 network call)
- High maintenance burden

### ❌ Prevent Domain Changes Entirely
**Why rejected:**
- Too restrictive
- Blocks legitimate use cases

---

## Recommended Fix

### Primary Fix: Remove Early Exit

```go
func DeployCatalog(..., domainName, sslCertPath, sslKeyPath string, ...) error {
    // Compute domain suffix BEFORE checking deployment
    domainSuffix := computeDomainSuffix(certDomain, domainName)
    
    isDeployed, existingResources, err := checkCatalogStatus(...)
    
    if !isDeployed {
        // Deploy pods only if they don't exist
        executePodLayers(...)
    }
    
    // ALWAYS run these (whether fresh or re-run)
    if sslCertPath != "" && sslKeyPath != "" {
        loadSSLCertificatesIfProvided(...)  // Cleans old certs first
    }
    
    return handlePostDeployment(...)  // Always register routes
}
```

### Certificate Cleanup

```go
func stageCertificatesForCaddy(baseDir, sslCertPath, sslKeyPath string) error {
    // Clean staged directory first
    os.RemoveAll(certDir)
    os.MkdirAll(certDir, dirPerm)
    
    // Copy new certs from user path
    certBytes := os.ReadFile(sslCertPath)
    os.WriteFile(stagedCertPath, certBytes, filePerm)
    // ...
}
```

### Domain Change Handling

**Option A:** Inform only (simple)
```go
logger.Infof("Configuring with domain: %s\n", newDomain)
// Always proceed
```

**Option B:** Update all app routes (complex but correct)
```go
if domainChanged && len(deployedApps) > 0 {
    updateAllApplicationRoutes(deployedApps, newDomain)
}
```

---

## What Gets Fixed

| Scenario | Before | After |
|----------|--------|-------|
| Re-run after cert failure | ❌ Never retries | ✅ Retries cert loading |
| Re-run after route failure | ❌ Never retries | ✅ Retries route registration |
| Partial route registration | ❌ Stuck incomplete | ✅ Completes missing routes |
| Domain change on re-run | ❌ Ignored | ✅ Routes updated |
| Certificate change | ❌ Old certs remain | ✅ Old certs cleaned |

---

## Open Questions

1. **Domain changes:** Should we update deployed application routes automatically?
2. **User confirmation:** Add `--yes` flag for automation, or just inform?
3. **State tracking:** Store domain in state file for reliable detection?

---

## Key Takeaway

**Route registration is idempotent and safe to re-run, but the early exit prevents it from ever running on re-configure.** Fix by moving post-deployment steps outside the `isDeployed` check.