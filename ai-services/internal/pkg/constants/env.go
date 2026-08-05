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
)
