export const getDisplayName = (
  value: string | undefined,
  options: Array<{ id: string; text: string }>,
): string => {
  if (!value) return "";
  const option = options.find((opt) => opt.id === value);
  return option?.text || value;
};
