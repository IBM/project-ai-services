/**
 * Converts bytes to gigabytes (rounded)
 */
export const bytesToGB = (bytes: number): number => {
  return Math.round(bytes / 1024 ** 3);
};
