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
  schema: { properties?: Record<string, { default?: unknown } | undefined> },
  excludeKeys: Set<string> = new Set(["model"]),
): boolean {
  if (excludeKeys.has(key)) {
    return false;
  }

  const property = schema.properties?.[key];

  return shouldIncludeParam(value, property);
}

/**
 * Splits serviceConfig.params into service-backend fields and credential fields.
 * Provider schema is the authoritative classifier: keys present there are credentials.
 * Service schema is a fallback when no provider schema is supplied.
 */
export function splitServiceParams(
  allParams: Record<string, unknown>,
  serviceSchema: { properties?: Record<string, unknown> } | null,
  providerSchema?: { properties?: Record<string, unknown> } | null,
): {
  serviceBackendParams: Record<string, unknown>;
  inferenceCredentialParams: Record<string, unknown>;
} {
  if (!allParams || Object.keys(allParams).length === 0) {
    return { serviceBackendParams: {}, inferenceCredentialParams: {} };
  }

  const providerKeys = new Set(
    providerSchema?.properties ? Object.keys(providerSchema.properties) : [],
  );
  const serviceKeys = new Set(
    serviceSchema?.properties ? Object.keys(serviceSchema.properties) : [],
  );

  const serviceBackendParams: Record<string, unknown> = {};
  const inferenceCredentialParams: Record<string, unknown> = {};

  for (const [key, value] of Object.entries(allParams)) {
    const isCredential =
      providerKeys.size > 0 ? providerKeys.has(key) : !serviceKeys.has(key);
    if (isCredential) {
      inferenceCredentialParams[key] = value;
    } else {
      serviceBackendParams[key] = value;
    }
  }

  return { serviceBackendParams, inferenceCredentialParams };
}
