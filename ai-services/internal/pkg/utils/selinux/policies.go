package selinux

// VFIOPolicyContent defines the SELinux policy for VFIO device access.
// This allows containers with container_t type to access VFIO devices.
const VFIOPolicyContent = `
module vllm_vfio_policy 1.0;

require {
    type container_t;
    type vfio_device_t;
    class chr_file { ioctl open read write getattr };
}

# Allow container_t (vLLM) to access vfio_device_t
allow container_t vfio_device_t:chr_file { ioctl open read write getattr };
`

// RootPodmanSocketPolicyContent defines the SELinux policy for root Podman socket access.
// These policies are generated using the udica tool from an existing running container.
// "podman inspect container > container.json && udica -j container.json container_policy.cil"
// The generated policies will have more entries which can be further trimmed based on the analysis.
// 1. vfio_device_t is used to allow the ai_services_root.process label to access the vfio device.
// 2. sock related policies are used to allow the ai_services_root.process label to access the podman socket.
// 3. sysfs_t is used to allow the ai_services_root.process label to access the /sys/ mount as readonly.
const RootPodmanSocketPolicyContent = `
(block ai_services_root
    (blockinherit container)
    (blockinherit net_container)
    (allow process vfio_device_t ( blk_file ( getattr read write append ioctl lock open )))
    (allow process vfio_device_t ( chr_file ( getattr read write append ioctl lock open )))
    (allow process var_run_t ( sock_file ( getattr open read write )))
    (allow process container_runtime_t (unix_stream_socket (connectto)))
    (allow process sysfs_t ( dir ( getattr ioctl lock open read search )))
    (allow process sysfs_t ( file ( getattr ioctl lock open read )))
    (allow process sysfs_t ( fifo_file ( getattr open read lock ioctl )))
    (allow process sysfs_t ( sock_file ( getattr open read )))
)
`

// RootlessPodmanSocketPolicyContent defines the SELinux policy for rootless Podman socket access.
// 1. vfio_device_t is used to allow the ai_services_nonroot.process label to access the vfio device.
// 2. sock related policies are used to allow the ai_services_nonroot.process label to access the podman socket.
// 3. sysfs_t is used to allow the ai_services_nonroot.process label to access the /sys/ mount as readonly.
// 4. unconfined_r is the role used for rootless containers.
const RootlessPodmanSocketPolicyContent = `
(block ai_services_nonroot
    (blockinherit container)
    (blockinherit net_container)
    (allow process vfio_device_t ( blk_file ( getattr read write append ioctl lock open )))
    (allow process vfio_device_t ( chr_file ( getattr read write append ioctl lock open )))
    (allow process user_tmp_t ( sock_file ( getattr open read write )))
    (allow process container_runtime_t (unix_stream_socket (connectto)))
    (allow process sysfs_t ( dir ( getattr ioctl lock open read search )))
    (allow process sysfs_t ( file ( getattr ioctl lock open read )))
    (allow process sysfs_t ( fifo_file ( getattr open read lock ioctl )))
    (allow process sysfs_t ( sock_file ( getattr open read )))
    (roletype unconfined_r process)
)
`

// CILPolicyContent defines the list of CIL policy names to be loaded.
var CILPolicyContent = []string{"ai_services_root_policy", "ai_services_nonroot_policy"}

// Made with Bob
