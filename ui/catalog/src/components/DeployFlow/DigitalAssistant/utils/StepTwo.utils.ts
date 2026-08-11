/**
 * Returns a generic label for accelerators
 */
export const getAcceleratorLabel = (acceleratorKey: string): string => {
  // For now, return generic label. In future, could map specific accelerator types
  // e.g., "nvidia-gpu" -> "NVIDIA GPU", "amd-gpu" -> "AMD GPU"
  return acceleratorKey ? "Accelerators" : "Accelerators";
};
