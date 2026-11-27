package constants

type Env string

const (
	// PCIAddressKey is the env var injected by Podman into containers for Spyre card PCI addresses.
	PCIAddressKey Env = "AIU_PCIE_IDS"

	// PCIDeviceEnvKey is the env var injected by the Kubernetes Spyre device plugin into
	// running containers. It is only present in the live container environment (not the pod spec)
	// and holds a comma-separated list of PCI addresses, e.g.:
	//   PCIDEVICE_IBM_COM_AIU_PF=0182:60:00.0,0183:70:00.0
	PCIDeviceEnvKey Env = "PCIDEVICE_IBM_COM_AIU_PF"

	// ROCRVisibleDevicesKey is the env var injected into AMD GPU containers to pin
	// them to specific GPU device ordinals (e.g. "0,1"), preventing two vLLM pods from
	// sharing GPUs. ROCm accepts only ordinals or GPU-<hex UUID> here, not PCI addresses.
	ROCRVisibleDevicesKey Env = "ROCR_VISIBLE_DEVICES"
)

const (
	// SpyreResourceName is the Kubernetes extended resource name advertised by the Spyre device plugin.
	SpyreResourceName = "ibm.com/spyre_pf"

	// AMDGPUResourceName is the resource key used to report AMD GPU accelerators in system info.
	AMDGPUResourceName = "amd.com/gpu"

	// AMDGPUProviderID is the component provider ID for AMD GPU vLLM deployments.
	AMDGPUProviderID = "vllm-rocm"
)
