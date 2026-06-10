import {
  SERVICE_ID_MAP,
  type ServiceKey,
} from "@/constants/services.constants";

/**
 * Maps service display names to their internal keys
 */
export const getServiceKey = (serviceName: string): ServiceKey => {
  const keyMap: Record<string, ServiceKey> = {
    "Digitize documents": "digitizeDocuments",
    "Find similar items": "findSimilarItems",
    "Question and answer": "questionAndAnswer",
    Summarization: "summarization",
  };
  return keyMap[serviceName] || "digitizeDocuments";
};

/**
 * Maps service keys to their service IDs used in the API
 */
export const getServiceIdFromKey = (serviceKey: ServiceKey): string => {
  return SERVICE_ID_MAP[serviceKey];
};

/**
 * Returns a generic label for accelerators
 */
export const getAcceleratorLabel = (_acceleratorKey: string): string => {
  return "Accelerators";
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

/**
 * Set of providers that share resources by model
 */
export const SHARED_BY_MODEL_PROVIDERS = new Set([
  "vllm-cpu",
  "vllm-spyre",
  "watsonx",
]);

// Made with Bob
