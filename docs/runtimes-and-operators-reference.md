# Runtimes, Operators, and Dependencies Reference

This document provides a comprehensive reference of container image versions, operator configurations, library dependencies, and host-level prerequisites for deploying AI Services across both **Podman** and **OpenShift** runtimes.

Optimized for **IBM Power Systems (ppc64le)** and **IBM Spyre™ AI Accelerators**, these services are managed dynamically via the AI Services Catalog.

---

## 1. High-Level Version Quick-Reference

This section serves as an executive quick-reference for the core components and operator versions required for both deployment runtimes.

### 1.1 Core Image and Operator Matrix

| Component / Operator | Supported Runtime(s) | Channel / Version | Image & Registry Path |
| :--- | :--- | :--- | :--- |
| **vLLM (IBM Spyre AIU)** | Podman & OpenShift | `v3.5.0` (RHAIIS) | `registry.redhat.io/rhaii/vllm-spyre-rhel9:3.5.0` |
| **vLLM (CPU-Only)** | Podman | `v0.28.0` | `icr.io/ppc64le-oss/vllm-ppc64le:0.28.0` |
| **vLLM (CPU-Only)** | OpenShift | `v0.19.1` | `icr.io/ppc64le-oss/vllm-ppc64le:0.19.1` |
| **IBM Spyre Operator** | OpenShift | `stable-v1.3` (v1.3.1) | Managed via Subscription |
| **Red Hat OpenShift AI (RHOAI)** | OpenShift | `stable-3.4` (v3.4.2) | Managed via Subscription |

---

## 2. Detailed OpenShift Operators & Prerequisites

When bootstrapping the **OpenShift** runtime environment, OLM (Operator Lifecycle Manager) subscriptions deploy several supporting operators to manage hardware feature discovery, scheduling, certs, routing, and the service mesh.

All operators are declared in the AI Services prerequisites at `ai-services/assets/bootstrap/openshift/02-operators/`.

### 2.1 Operator OLM Subscription Details

| Operator Name | Subscription Key | Namespace | Channel | Starting CSV | Catalog Source | Source Namespace |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **IBM Spyre Operator** | `spyre-operator` | `spyre-operator` | `stable-v1.3` | `spyre-operator.v1.3.1` | `certified-operators` | `openshift-marketplace` |
| **Red Hat OpenShift AI (RHODS)** | `rhods-operator` | `redhat-ods-operator` | `stable-3.4` | `rhods-operator.3.4.2` | `redhat-operators` | `openshift-marketplace` |
| **OpenShift Service Mesh 3.x** | `servicemeshoperator3` | `openshift-operators` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Node Feature Discovery (NFD)** | `nfd` | `openshift-nfd` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Secondary Scheduler** | `openshift-secondary-scheduler-operator` | `openshift-secondary-scheduler-operator` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Cert-Manager** | `openshift-cert-manager-operator` | `cert-manager-operator` | `stable-v1` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |

### 2.2 Operator-Managed Operands and Custom Resources

Once the subscriptions are healthy, the following configuration operands are applied to initialize the AI serving stack:

1. **`DSCInitialization` (Data Science Initializer):**
   - Deploys `default-dsci` in `redhat-ods-applications` and `redhat-ods-monitoring`.
   - Links the OpenShift Service Mesh control plane in the `istio-system` namespace.
2. **`DataScienceCluster` (RHOAI stack):**
   - Configures `default-dsc` with the **KServe** component enabled (`managementState: Managed`) to manage ServingRuntimes.
3. **`ServingRuntime` Custom Resources:**
   - **`spyre-temp`**: ServingRuntime for vLLM on Spyre AIU cards using `registry.redhat.io/rhaii/vllm-spyre-rhel9:3.5.0`.
   - **`cpu-temp`**: ServingRuntime for vLLM on CPU using `icr.io/ppc64le-oss/vllm-ppc64le:0.19.1`.
4. **`InferenceService` (KServe):**
   - Creates the model predictor pods, mapping them to the specific `ServingRuntime` and applying resources/limits requests (e.g. `ibm.com/spyre_pf` requests/limits).

---

## 3. Detailed Podman Host-Level Dependencies

The local **Podman** runtime executes on bare-metal or LPAR instances of **Red Hat Enterprise Linux (RHEL 9) on ppc64le** with active Red Hat Network (RHN) subscriptions. Instead of operators, the system relies on host-level driver configurations, udev rules, and system groups to safely bridge hardware accelerators to rootless container pods.

These requirements are checked and automatically repaired by the `ai-services bootstrap validate` and `ai-services bootstrap configure` subcommands.

### 3.1 Kernel, Driver, and Device Node Configurations

| Configuration Item | Host Path / Command | Expected Value / Details | Purpose |
| :--- | :--- | :--- | :--- |
| **VFIO IDs Option** | `/etc/modprobe.d/vfio-pci.conf` | `options vfio-pci ids=1014:06a7,1014:06a8` | Registers IBM Spyre Vendor ID (`1014`) and Device IDs (`06a7` Rev1, `06a8` Rev2). |
| **VFIO Idle Option** | `/etc/modprobe.d/vfio-pci.conf` | `options vfio-pci disable_idle_d3=yes` | Disables idle D3 power saving state on the AIU cards. |
| **Kernel Module Load** | `/etc/modules-load.d/vfio-pci.conf` | `vfio-pci`<br>`vfio_iommu_spapr_tce` | Loads required kernel drivers to manage memory page tables (SPAPR IOMMU) for Power. |
| **Udev Device Rule 1** | `/etc/udev/rules.d/95-vfio-3.rules` | `SUBSYSTEM=="vfio", ACTION=="add\|change", GROUP="sentient", MODE="0660", SECLABEL{selinux}="system_u:object_r:vfio_device_t:s0"` | Maps vfio-pci subsystem device nodes under `/dev/vfio/*` to the sentient group. |
| **Udev Device Rule 2** | `/etc/udev/rules.d/95-vfio-3.rules` | `KERNEL=="vfio", SUBSYSTEM=="misc", ACTION=="add\|change", GROUP="sentient", MODE="0660", SECLABEL{selinux}="system_u:object_r:vfio_device_t:s0"` | Grants identical permissions and SELinux contexts to root misc devices. |

### 3.2 User, Group, and Supplementary Group Access

In rootless Podman deployments, direct container workloads are run by local users. To enable permission inheritance:

1. **Sentient Group:**
   - The host system must declare a `sentient` group (typically GID is synchronized across active nodes).
2. **Systemd Unit Configurations:**
   - Both `podman.service` and `podman-restart.service` must be configured with `SupplementaryGroups=sentient`. This allows the rootless podman daemon socket to access hardware handles.
3. **Container ulimits and namespace annotations:**
   - Podman container launches map host configurations utilizing the following annotations:
     - `io.podman.annotations.ulimit="nofile=134217728:134217728,memlock=-1:-1"` (raises file descriptors and locked-memory bounds).
     - `io.podman.annotations.userns="keep-id"` (preserves current host user namespace map inside the container).
     - `run.oci.keep_original_groups="1"` (preserves host groups within the container, enabling `/dev/vfio` pass-through).
     - `podman.io/device=/dev/vfio` resource request (injects character device drivers).

### 3.3 SELinux Policies

To secure the environment without disabling SELinux, a minimal SELinux security module is compiled and loaded into the kernel during bootstrap:
- **Policy Module:** `spyre-device-plugin-selinux-minimal`
- **Enforcement:** Restricts container workloads to the `vfio_device_t` type context while allowing safe socket and memory sharing.

---

Made with IBM Bob
