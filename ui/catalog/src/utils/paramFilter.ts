/**
 * Filters out empty values and values matching schema defaults
 * Handles boolean false and number 0 as valid values
 */
export function shouldIncludeParam(
  value: unknown,
  schemaProperty: { default?: unknown } | undefined,
): boolean {
  const hasValue =
    value !== undefined &&
    value !== null &&
    (typeof value === "boolean" || typeof value === "number" || value !== "");

  if (!hasValue) {
    return false;
  }

  if (schemaProperty?.default !== undefined) {
    return value !== schemaProperty.default;
  }

  return true;
}

/**
 * Checks if a parameter should be displayed in UI
 * Excludes specific keys and applies shouldIncludeParam logic
 */
export function shouldShowParam(
  key: string,
  value: unknown,
  schema: { properties?: Record<string, { default?: unknown }> },
  excludeKeys: Set<string> = new Set(["model"]),
): boolean {
  if (excludeKeys.has(key)) {
    return false;
  }

  const property = (
    schema.properties as Record<string, { default?: unknown }>
  )?.[key];

  return shouldIncludeParam(value, property);
}
