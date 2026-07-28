export const getResourceStatus = (
  required: string,
  available: string,
): "sufficient" | "insufficient" | "unknown" => {
  if (available === "N/A") return "unknown";

  const req = parseFloat(required);
  const avail = parseFloat(available);

  return avail >= req ? "sufficient" : "insufficient";
};

export const bytesToGB = (bytes: number): number => {
  return Math.round(bytes / 1024 ** 3);
};
