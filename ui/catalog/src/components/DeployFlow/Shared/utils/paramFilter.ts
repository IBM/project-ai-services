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

// Service schemas nest params under a wrapper object (e.g. backend.properties).
// This collects the leaf-level keys so classification works correctly.
function extractServiceParamKeys(
  schema: { properties?: Record<string, unknown> } | null,
): Set<string> {
  if (!schema?.properties) return new Set();

  const keys = new Set<string>();
  for (const [key, value] of Object.entries(schema.properties)) {
    const prop = value as {
      type?: string;
      properties?: Record<string, unknown>;
    };
    if (prop?.type === "object" && prop.properties) {
      for (const nestedKey of Object.keys(prop.properties)) {
        keys.add(nestedKey);
      }
    } else {
      keys.add(key);
    }
  }
  return keys;
}

// Looks up a param's schema definition, searching inside nested wrappers first.
function getServiceSchemaProperty(
  key: string,
  schema: { properties?: Record<string, unknown> } | null,
): { default?: unknown } | undefined {
  if (!schema?.properties) return undefined;

  for (const value of Object.values(schema.properties)) {
    const prop = value as {
      type?: string;
      properties?: Record<string, unknown>;
    };
    if (prop?.type === "object" && prop.properties && key in prop.properties) {
      return prop.properties[key] as { default?: unknown } | undefined;
    }
  }
  return schema.properties[key] as { default?: unknown } | undefined;
}

/**
 * Splits allParams into service-backend params and inference credential params.
 * Keys in providerSchema are credentials; everything else is a service param.
 * Falls back to serviceSchema when no providerSchema is supplied.
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
  const serviceKeys = extractServiceParamKeys(serviceSchema);

  const serviceBackendParams: Record<string, unknown> = {};
  const inferenceCredentialParams: Record<string, unknown> = {};

  for (const [key, value] of Object.entries(allParams)) {
    const isCredential =
      providerKeys.size > 0 ? providerKeys.has(key) : !serviceKeys.has(key);

    const schemaProperty = isCredential
      ? (providerSchema?.properties?.[key] as { default?: unknown } | undefined)
      : getServiceSchemaProperty(key, serviceSchema);

    if (!shouldIncludeParam(value, schemaProperty)) {
      continue;
    }

    if (isCredential) {
      inferenceCredentialParams[key] = value;
    } else {
      serviceBackendParams[key] = value;
    }
  }

  return { serviceBackendParams, inferenceCredentialParams };
}
