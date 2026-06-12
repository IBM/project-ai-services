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
5. While running `ai-services catalog configure` cmd, make sure to add `--https-port 8443`, as 443 is a privileged port. For PowerVS environments, use `--https-port 6443` along with a custom domain configuration, as 6443 is one of the available port option for non-root users on PowerVS.

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
Created custom SELinux policy modules that are automatically applied during bootstrap:

**1. VFIO Device Access Policy (`vllm_vfio_policy`)**:
- Traditional `.te` format policy
- Allows `container_t` type to access `vfio_device_t` devices
- Grants minimal required permissions: `ioctl`, `open`, `read`, `write`, `getattr`
- Applied during `ai-services bootstrap` for Spyre card access
- Automatically reloads udev rules after policy installation to apply SELinux labels to existing devices

**2. Non-Root Podman Socket Access Policy (`ai_services_nonroot_policy`)**:
- CIL (Common Intermediate Language) format policy generated using udica tool
- Creates custom `ai_services_nonroot.process` label that inherits from `container` and `net_container` templates
- Comprehensive permissions including:
  - VFIO device access (block and character files)
  - Non-root Podman socket access (`user_tmp_t` socket files at `/run/user/<uid>/podman/podman.sock`)
  - Container runtime connection (`container_runtime_t` Unix stream socket)
  - Read-only access to `/sys/` filesystem (`sysfs_t`)
  - Uses `unconfined_r` role for non-root container operations
- Applied during `ai-services bootstrap`

### Policy Generation and Format
The non-root Podman socket policy was generated using the **udica** tool:
```bash
podman inspect container > container.json
udica -j container.json container_policy.cil
```

The generated policy uses CIL format and inherits from udica's base templates:
- `/usr/share/udica/templates/base_container.cil` - Base container permissions
- `/usr/share/udica/templates/net_container.cil` - Network container permissions

### Policy Application Process
SELinux policies are applied using different methods based on their format:

**For Traditional `.te` Format (VFIO policy)**:
1. **Policy Compilation**: The policy source (`.te` file) is compiled into a module (`.mod`) using `checkmodule`
2. **Policy Packaging**: The module is packaged into a policy package (`.pp`) using `semodule_package`
3. **Policy Installation**: The package is installed using `semodule -i`, with automatic removal of existing versions if present
4. **Device Labeling**: Udev rules are reloaded to apply SELinux labels to existing devices

**For CIL Format (Non-root Podman socket policy)**:
1. **Policy Writing**: The CIL policy content is written to a `.cil` file
2. **Direct Installation**: The policy is installed directly using `semodule -i` with udica template dependencies:
   ```bash
   semodule -i ai_services_nonroot_policy.cil \
     /usr/share/udica/templates/base_container.cil \
     /usr/share/udica/templates/net_container.cil
   ```
3. **Automatic Removal**: Existing versions are automatically removed before installation if present

All policies are applied automatically during the bootstrap process and persist across system reboots.

### Configuration Changes
**Container Templates** (`vllm-server.yaml.tmpl`):
- Containers can use custom SELinux label `ai_services_nonroot.process`
- Only containers using Spyre cards or requiring Podman socket access receive custom security contexts
- CPU-only containers use default context

### Verification
```bash
# Check policy installations
sudo semodule -l | grep -E 'vllm_vfio_policy|ai_services_nonroot_policy'

# Verify VFIO device labels
ls -Z /dev/vfio/

# Check SELinux status
getenforce
sestatus

# Verify policy modules are loaded
sudo semodule -l | grep vllm_vfio_policy           # VFIO device access
sudo semodule -l | grep ai_services_nonroot_policy # Non-root Podman socket
```

---

## 2. Podman Socket Configuration

### Problem
Non-root users need proper Podman socket configuration to run containers. When bootstrap is run with sudo, the Podman socket must be configured for the actual user (not root).

### Error
```
Error: unable to connect to Podman: unix:///run/user/<uid>/podman/podman.sock: connect: permission denied
```

### Solution Implemented
**Function**: `setupPodman()` in `internal/pkg/bootstrap/podman/helper.go`

**Logic**:
```
When running via sudo (SUDO_USER environment variable set):
  → systemctl enable podman.socket --now --machine=username@.host --user
```

**Detection Method**:
- `SUDO_USER` environment variable - Identify the actual user when bootstrap is run with sudo
- Configures user-specific Podman socket at `/run/user/<uid>/podman/podman.sock`

**Additional Configuration**:
- **Socket Resolution**: Automatically resolves the correct Podman socket path for non-root user
- **Auth File Path**: Resolves Podman auth file path for non-root user context
- **Rootless Support**: Full support for rootless Podman operations with proper socket access

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

## 6. Privileged Port Restrictions for Non-Root Users

### Problem
Non-root users cannot bind to privileged ports (ports below 1024) due to Linux security restrictions. This affects services that traditionally use standard ports like 443 (HTTPS).

### Error
```
ERRO[0003] "rootlessport cannot expose privileged port 443, you can add 'net.ipv4.ip_unprivileged_port_start=443' to /etc/sysctl.conf (currently 1024), or choose a larger port number (>= 1024): listen tcp 0.0.0.0:443: bind: permission denied"
Error: unable to start container "f72329a97540825bb753ec41806021c9fd15890ae54f6874b2f13e2c6bc8890a": starting some containers: internal libpod error
```

### Solution Implemented
**Requirement**: Users must specify a non-privileged port when configuring the catalog service.

**Usage**:
```bash
ai-services catalog configure --https-port 8443
```

**Rationale**:
- Port 8443 is a common alternative to 443 for HTTPS services
- Avoids requiring system-level configuration changes (`net.ipv4.ip_unprivileged_port_start`)

---

## Security Considerations

### SELinux Policy
- **Minimal Permissions**: Each policy grants only the necessary permissions for its specific purpose:
  - VFIO policy: Only device access operations (ioctl, open, read, write, getattr)
  - Non-root Podman socket policy: Comprehensive but targeted permissions for container operations, VFIO access, socket communication, and read-only sysfs access
- **Custom Process Label**: CIL policy creates dedicated process label `ai_services_nonroot.process` for non-root containers
- **Template Inheritance**: CIL policy inherits from udica's proven templates:
  - `base_container.cil` - Core container permissions
  - `net_container.cil` - Network container permissions
- **Automatic Application**: Policies are automatically compiled/written, packaged (for .te), and installed during bootstrap
- **Persistent Configuration**: Policies survive reboots and are reinstalled if already present
- **Audit Trail**: SELinux logs all access attempts for security monitoring
- **Device Labeling**: VFIO policy automatically triggers udev rule reload to label existing devices
- **Format Flexibility**: Supports both traditional `.te` format and modern CIL format policies
- **Rootless Role**: Uses `unconfined_r` role for non-root container operations

### Container Security
- **Container-Level Context**: Security applied per container, not per Pod
- **Principle of Least Privilege**: CPU-only containers use default context
- **No Privilege Escalation**: Containers run as non-root user inside
- **Device Access Control**: Only containers with proper annotations get VFIO access
- **User Namespace Mapping**: Removed dependency on `keep-id` user namespace mapping by handling Podman socket access through SELinux policy configuration, improving security and compatibility

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

# Verify policy installations
sudo semodule -l | grep -E 'vllm_vfio_policy|ai_services_nonroot_policy'

# Check individual policies
sudo semodule -l | grep vllm_vfio_policy           # VFIO device access policy
sudo semodule -l | grep ai_services_nonroot_policy # Non-root Podman socket policy

# Check VFIO device labels
sudo ls -Z /dev/vfio/

# Check non-root Podman socket label
ls -Z /run/user/$(id -u)/podman/podman.sock

# View SELinux status
getenforce
sestatus

# Check if SELinux is blocking container operations
sudo ausearch -m avc -c container_t -ts recent
```

### Podman Diagnostics
```bash
# Check non-root socket status
systemctl --user status podman.socket

# Verify non-root socket file
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