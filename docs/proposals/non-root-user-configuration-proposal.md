# Non-Root User Configuration Proposal

> **Note:** This document is specific to the **Podman runtime** configuration. The configurations and requirements described here apply to Podman-based deployments only.

## Overview

This document outlines the configuration changes and system requirements implemented to enable non-root users to run AI workloads with Spyre cards securely and efficiently on the AI Services platform using Podman runtime.

---

## Prerequisites Summary

1. Run these commands before running the bootstrap command:
   ```bash
   sudo usermod -aG wheel <username>
   sudo loginctl enable-linger <username>
   ```
2. Run bootstrap command with sudo:
   ```bash
   sudo ai-services bootstrap
   ```

3. Create application data directory (if using default location):
   ```bash
   sudo mkdir -p /var/lib/ai-services/{models,cache,data}
   sudo chown -R <username>:sentient /var/lib/ai-services
   ```

4. Apply configuration changes for users:
   ```bash
   # Terminate user session to apply group membership and resource limits
   sudo loginctl terminate-user <username>
   ```

   Then log back in.

   ```
   # Enable lingering to keep user services running
   sudo loginctl enable-linger <username>
   ```

**Why This Is Required:**
- Group membership changes require a new login session
- Resource limits (nofile, memlock) are applied at login time
- Systemd user slice limits need the user session to restart
- `loginctl terminate-user` ensures a clean session restart
- `loginctl enable-linger` allows user services to persist after logout

---

## 1. SELinux VFIO Access Configuration

### Problem
vLLM containers were crashing with "Permission denied" errors when attempting to access VFIO devices (`/dev/vfio/*`) required for Spyre card operations. The root cause was SELinux blocking container access to VFIO devices.

### Error
```
The Linux VFIO kernel module should create a device node at /dev/vfio/vfio, however it was not found or accessible.

podman exec db950e66dc9e ls -la /dev/vfio/
ls: cannot access '/dev/vfio/vfio': Permission denied
ls: cannot access '/dev/vfio/3': Permission denied
ls: cannot access '/dev/vfio/2': Permission denied
ls: cannot access '/dev/vfio/1': Permission denied
ls: cannot access '/dev/vfio/0': Permission denied
total 0
drwxr-xr-x. 2 root root 140 Apr 27 11:53 .
drwxr-xr-x. 6 root root 360 Apr 27 11:53 ..
-?????????? ? ?    ?      ?            ? 0
-?????????? ? ?    ?      ?            ? 1
-?????????? ? ?    ?      ?            ? 2
-?????????? ? ?    ?      ?            ? 3
-?????????? ? ?    ?      ?            ? vfio
```

### Previous State
- Containers ran without explicit SELinux security context
- SELinux denied access to VFIO devices by default
- Containers could not access Spyre cards, causing crashes

### Solution Implemented
Created custom SELinux policy module `vllm_vfio_policy` that:
- Allows `container_t` type to access `vfio_device_t` devices
- Grants minimal required permissions: `ioctl`, `open`, `read`, `write`, `getattr`
- Sets persistent file context rules: `/dev/vfio/*` → `vfio_device_t`

### Configuration Changes
**Container Templates** (`vllm-server.yaml.tmpl`):
- Added explicit `container_t` SELinux type at container level (not Pod level)
- Only containers using Spyre cards receive the security context
- CPU-only containers use default context

### Verification
```bash
# Check policy installation
sudo semodule -l | grep vllm_vfio_policy

# Verify file context rules
sudo semanage fcontext -l | grep vfio

# Check device labels
ls -Z /dev/vfio/
```

---

## 2. Podman Socket Configuration

### Problem
Podman must be configured differently based on execution context:
- Root user (no sudo): System-wide podman socket
- Sudo user: User-specific podman socket for the actual user

### Error
```
Error: unable to connect to Podman: unix:///run/podman/podman.sock: connect: permission denied
```

### Solution Implemented
**Function**: `setupPodman()` in `internal/pkg/bootstrap/podman/helper.go`

**Logic**:
```
If running as root AND not via sudo:
  → systemctl enable podman.socket --now
  
If running via sudo (SUDO_USER environment variable set):
  → systemctl enable podman.socket --now --machine=username@.host --user
```

**Detection Method**:
- `os.Geteuid()` - Check effective user ID
- `SUDO_USER` environment variable - Identify original user when using sudo

---

## 3. Resource Limits and Systemd Service Configuration

### Problem
Containers require elevated resource limits and proper group membership:
- High file descriptor limits (`nofile`) for connection handling
- Unlimited memory lock (`memlock`) for GPU memory operations
- Podman service must inherit `sentient` group for VFIO device access

### Error
```
unable to start container "591bb94941": crun: setrlimit `RLIMIT_NOFILE`: Operation not permitted: OCI permission denied
```

### Solution Implemented

#### A. File Descriptor Limit (nofile)
**Configuration**: `/etc/security/limits.conf`
```
@sentient hard nofile 134217728
```
- Applies to all users in `sentient` group

#### B. Memory Lock Limit (memlock)
**Configuration**: `/etc/security/limits.d/memlock.conf`
```
@sentient - memlock unlimited
```
- Required for direct hardware access

#### C. Systemd Service Group Inheritance
**Problem**: Podman invoked via systemd socket doesn't inherit user's supplementary groups

**Configuration**: `/etc/systemd/system/podman.service.d/override.conf`
```ini
[Service]
SupplementaryGroups=sentient
```

**Repair Process**:
1. Create systemd drop-in directory
2. Write override configuration
3. Reload systemd daemon: `systemctl daemon-reload`
4. Restart podman services: `systemctl restart podman.service podman.socket`

---

## 4. Directory Access Requirements

### Problem
Applications need persistent storage for models, cache, and data files.

### Error
```
user does not have write permission to directory: /var/lib/ai-services
```

### Solution: Base Directory Configuration
Applications can be created with a custom base directory using the `--baseDir` flag. The default base directory is `/var/lib/ai-services`, but users can specify an alternative location.

**Prerequisites**:
- The user must have write permissions to the specified base directory
- If using the default `/var/lib/ai-services`, ensure proper permissions are set (see setup instructions below)
- If using a custom directory, verify permissions before creating the application

**Required Directory Structure** (example for default location):
```
/var/lib/ai-services/
├── models/     # Model files
├── cache/      # Temporary cache
└── data/       # Application data
```

**Setup Commands** (for default location):
```bash
# Create directory structure
sudo mkdir -p /var/lib/ai-services/{models,cache,data}

# Set ownership (replace user:group appropriately)
sudo chown -R <user>:sentient /var/lib/ai-services

# Set permissions
sudo chmod -R 755 /var/lib/ai-services
```

**Rationale**:
- Default `/var/lib/ai-services` is not automatically created by ai-services tool (requires admin decision on ownership)
- Custom directories allow users to work without requiring sudo/admin privileges
- Documented as prerequisite in user guide

---

## 5. SMT Level Configuration

### Problem
SMT level affects CPU performance for Spyre card operations. Previously configured as part of application create. Since application create run in rootless mode, SMT configuration will fail.

### Solution Implemented
**Function**: `setupSMTLevel()` in `internal/pkg/bootstrap/podman/helper.go`

**Integration**:
- Part of `ai-services bootstrap configure` command
- Runs automatically during LPAR configuration
- Persists across system reboots via systemd service

**Target Value**: SMT=2 (optimal for Spyre card performance)

---

## Security Considerations

### SELinux Policy
- **Minimal Permissions**: Only grants necessary VFIO device access
- **Targeted Policy**: Applies only to `container_t`, not system-wide
- **Persistent Configuration**: File context rules survive reboots
- **Audit Trail**: SELinux logs all access attempts

### Container Security
- **Container-Level Context**: Security applied per container, not per Pod
- **Principle of Least Privilege**: CPU-only containers use default context
- **No Privilege Escalation**: Containers run as non-root user inside
- **Device Access Control**: Only containers with proper annotations get VFIO access

### Group Membership
- **Explicit Assignment**: Users must be explicitly added to `sentient` group
- **Audit Trail**: Group membership changes logged by system
- **Revocable Access**: Remove user from group to revoke VFIO access

---
## Troubleshooting Commands

### SELinux Diagnostics
```bash
# Check for SELinux denials
sudo ausearch -m avc -ts recent

# Verify policy installation
sudo semodule -l | grep vllm_vfio_policy

# Check file contexts
sudo ls -Z /dev/vfio/

# View SELinux status
getenforce
sestatus
```

### Podman Diagnostics
```bash
# Check socket status
systemctl status podman.socket
systemctl --user status podman.socket

# Verify socket file
ls -l /run/podman/podman.sock
ls -l /run/user/$(id -u)/podman/podman.sock

# Test connection
podman ps
podman version
```

### Group Membership Diagnostics
```bash
# Verify user groups
groups <username>
id <username>

# Check sentient group members
getent group sentient

# Verify effective groups (after login)
id
groups
```

### Resource Limits Diagnostics
```bash
# Check current limits
ulimit -Hn  # nofile
ulimit -l  # memlock

# Verify limits configuration
cat /etc/security/limits.conf | grep sentient
cat /etc/security/limits.d/memlock.conf
```

### System Configuration Diagnostics
```bash
# Check SMT level
ppc64_cpu --smt

# Verify systemd services
systemctl status smtstate.service
systemctl status podman.service
systemctl show podman.service | grep SupplementaryGroups

# Check directory permissions (default location)
ls -ld /var/lib/ai-services
ls -l /var/lib/ai-services/

# Check custom directory permissions
ls -ld /path/to/custom/directory
ls -l /path/to/custom/directory/
```

---