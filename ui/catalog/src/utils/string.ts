// Normalizes a string by converting to lowercase and replacing spaces with hyphens
export const normalizeString = (str: string): string => {
  return str.toLowerCase().replace(/\s+/g, "-");
};
