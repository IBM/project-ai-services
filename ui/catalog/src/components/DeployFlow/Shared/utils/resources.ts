// Determines if resources are sufficient, insufficient, or unknown
export const getResourceStatus = (
  required: string,
  available: string,
): "sufficient" | "insufficient" | "unknown" => {
  if (available === "N/A") return "unknown";

  const req = parseFloat(required);
  const avail = parseFloat(available);

  return avail >= req ? "sufficient" : "insufficient";
};

// Converts bytes to gigabytes (rounded)
export const bytesToGB = (bytes: number): number => {
  return Math.round(bytes / 1024 ** 3);
};

type ProviderResourceEntry = {
  cpu: number;
  memory: number;
  storage: number;
  accelerators: Record<string, number>;
};

// Sums a uniqueProviders map into CalculatedResources (bytes → GB for memory and storage)
export const sumProviderResources = (
  uniqueProviders: Record<string, ProviderResourceEntry>,
): {
  cpu: number;
  memory: number;
  accelerators: Record<string, number>;
  storage: number;
} => {
  let totalCPU = 0;
  let totalMemory = 0;
  let totalStorage = 0;
  const totalAccelerators: Record<string, number> = {};

  Object.values(uniqueProviders).forEach((r) => {
    totalCPU += r.cpu;
    totalMemory += r.memory;
    totalStorage += r.storage;
    Object.entries(r.accelerators).forEach(([key, count]) => {
      totalAccelerators[key] = (totalAccelerators[key] || 0) + count;
    });
  });

  return {
    cpu: totalCPU,
    memory: bytesToGB(totalMemory),
    accelerators: totalAccelerators,
    storage: bytesToGB(totalStorage),
  };
};
