package spyre

// vfioPolicyContent defines the SELinux policy for VFIO device access.
// This allows containers with container_t type to access VFIO devices.
const vfioPolicyContent = `
module vllm_vfio_policy 1.0;

require {
    type container_t;
    type vfio_device_t;
    class chr_file { ioctl open read write getattr };
}

# Allow container_t (vLLM) to access vfio_device_t
allow container_t vfio_device_t:chr_file { ioctl open read write getattr };
`

// podmanSocketPolicyContent defines the SELinux policy for Podman socket access.
// This allows containers with container_t type to access the Podman socket.
const podmanSocketPolicyContent = `
module podman_socket_policy 1.0;

require {
    type container_t;
    type var_run_t;
	type user_tmp_t;

    class sock_file { getattr read write open };
    class unix_stream_socket connectto;
    class dir search;
}

# ------------------------------------------------------------------
# Root/system Podman socket
# Example:
#   /run/podman/podman.sock
# SELinux type:
#   var_run_t
# ------------------------------------------------------------------

allow container_t var_run_t:sock_file { getattr read write open };
allow container_t var_run_t:unix_stream_socket connectto;

# ------------------------------------------------------------------
# Rootless Podman socket
# Example:
#   /run/user/<uid>/podman/podman.sock
# SELinux type:
#   user_tmp_t
# ------------------------------------------------------------------

allow container_t user_tmp_t:sock_file { getattr read write open };
allow container_t user_tmp_t:unix_stream_socket connectto;
allow container_t user_tmp_t:dir search;
`

// Made with Bob
