/**
 * Returns a generic label for accelerators
 */
export const getAcceleratorLabel = (acceleratorKey: string): string => {
  // For now, return generic label. In future, could map specific accelerator types
  // e.g., "nvidia-gpu" -> "NVIDIA GPU", "amd-gpu" -> "AMD GPU"
  return acceleratorKey ? "Accelerators" : "Accelerators";
};

/**
 * Determines if resources are sufficient, insufficient, or unknown
 */
export const getResourceStatus = (
  required: string,
  available: string,
): "sufficient" | "insufficient" | "unknown" => {
  if (available === "N/A") return "unknown";

  const req = parseFloat(required);
  const avail = parseFloat(available);

  return avail >= req ? "sufficient" : "insufficient";
};

/**
 * Gets display name from an option ID
 */
export const getDisplayName = (
  value: string | undefined,
  options: Array<{ id: string; text: string }>,
): string => {
  if (!value) return "";
  const option = options.find((opt) => opt.id === value);
  return option?.text || value;
};

/**
 * Converts bytes to gigabytes (rounded)
 */
export const bytesToGB = (bytes: number): number => {
  return Math.round(bytes / 1024 ** 3);
};
