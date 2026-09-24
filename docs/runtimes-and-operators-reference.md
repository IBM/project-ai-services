# Runtimes, Operators, and Dependencies Reference

This document provides a comprehensive reference of container image versions, operator configurations, library dependencies, and host-level prerequisites for deploying AI Services across both **Podman** and **OpenShift** runtimes.

Optimized for **IBM Power Systems (ppc64le)** and **IBM Spyre™ AI Accelerators**, these services are managed dynamically via the AI Services Catalog.

---

## 1. High-Level Version Quick-Reference

This section serves as an executive quick-reference for the core components and operator versions required for both deployment runtimes.

### 1.1 Core Image and Operator Matrix

| Component / Operator | Supported Runtime(s) | Channel / Version | Image & Registry Path |
| :--- | :--- | :--- | :--- |
| **vLLM (IBM Spyre AIU)** | Podman & OpenShift | `v3.5.0` (Red Hat AI Inference) | `registry.redhat.io/rhaii/vllm-spyre-rhel9:3.5.0` |
| **vLLM (CPU-Only)** | Podman | `v0.28.0` | `icr.io/ppc64le-oss/vllm-ppc64le:0.28.0` |
| **vLLM (CPU-Only)** | OpenShift | `v0.19.1` | `icr.io/ppc64le-oss/vllm-ppc64le:0.19.1` |
| **IBM Spyre Operator** | OpenShift | `stable-v1.3` (v1.3.1) | Managed via Subscription |
| **Red Hat OpenShift AI (RHOAI)** | OpenShift | `stable-3.5` (v3.5.0) | Managed via Subscription |

---

## 2. Detailed OpenShift Operators & Prerequisites

When bootstrapping the **OpenShift** runtime environment, OLM (Operator Lifecycle Manager) subscriptions deploy several supporting operators to manage hardware feature discovery, scheduling, certs, routing, and the service mesh.

All operators are declared in the AI Services prerequisites at `ai-services/assets/bootstrap/openshift/02-operators/`.

### 2.1 Operator OLM Subscription Details

| Operator Name | Subscription Key | Namespace | Channel | Starting CSV | Catalog Source | Source Namespace |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **IBM Spyre Operator** | `spyre-operator` | `spyre-operator` | `stable-v1.3` | `spyre-operator.v1.3.1` | `certified-operators` | `openshift-marketplace` |
| **Red Hat OpenShift AI (RHODS)** | `rhods-operator` | `redhat-ods-operator` | `stable-3.5` | `rhods-operator.3.5.0` | `redhat-operators` | `openshift-marketplace` |
| **OpenShift Service Mesh 3.x** | `servicemeshoperator3` | `openshift-operators` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Node Feature Discovery (NFD)** | `nfd` | `openshift-nfd` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Secondary Scheduler** | `openshift-secondary-scheduler-operator` | `openshift-secondary-scheduler-operator` | `stable` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |
| **Cert-Manager** | `openshift-cert-manager-operator` | `cert-manager-operator` | `stable-v1` | *Latest stable* | `redhat-operators` | `openshift-marketplace` |

### 2.2 RHOAI/RHODS Operator-Managed Operands and Custom Resources

Once the subscriptions are healthy, the following Red Hat OpenShift AI (RHOAI / RHODS) operator-managed configuration operands and custom resources are applied to initialize the AI serving stack:

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

Made with IBM Bob
