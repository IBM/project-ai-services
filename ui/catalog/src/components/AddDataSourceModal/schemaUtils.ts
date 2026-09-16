import type { ConnectorParamsSchema } from "./types";
export interface ConnectorField {
  key: string;
  label: string;
  description?: string;
  type: "text" | "password" | "checkboxArray";
  checkboxOptions?: string[];
  sectionTitle: string;
  isRequired: boolean;
  isOptional: boolean;
}

export function parseConnectorSchema(
  schema: ConnectorParamsSchema,
): ConnectorField[] {
  const required = new Set(schema.required ?? []);
  const fields: ConnectorField[] = [];

  for (const [key, property] of Object.entries(schema.properties)) {
    if (!property) continue;

    let type: ConnectorField["type"] = "text";
    let checkboxOptions: string[] | undefined;

    if (property.type === "array" && Array.isArray(property.items?.enum)) {
      type = "checkboxArray";
      checkboxOptions = property.items.enum;
    } else if (property.format === "password") {
      type = "password";
    }

    const isRequired = required.has(key);
    fields.push({
      key,
      label: property.title ?? key,
      description: property.description,
      type,
      checkboxOptions,
      sectionTitle: property["ui:section"] ?? "",
      isRequired,
      isOptional: !isRequired,
    });
  }

  return fields;
}

/**
 * Groups an ordered list of ConnectorFields by sectionTitle, preserving
 * insertion order.
 */
export function groupFieldsBySections(
  fields: ConnectorField[],
): Array<{ title: string; fields: ConnectorField[] }> {
  const sectionMap = new Map<string, ConnectorField[]>();

  for (const field of fields) {
    if (!sectionMap.has(field.sectionTitle)) {
      sectionMap.set(field.sectionTitle, []);
    }
    sectionMap.get(field.sectionTitle)!.push(field);
  }

  return Array.from(sectionMap.entries()).map(([title, sectionFields]) => ({
    title,
    fields: sectionFields,
  }));
}

/**
 * Builds the initial form values object from a list of ConnectorFields,
 * setting checkboxArray fields to [] and all others to "".
 */
export function buildInitialValues(
  fields: ConnectorField[],
): Record<string, string | string[]> {
  const values: Record<string, string | string[]> = {};
  for (const field of fields) {
    values[field.key] = field.type === "checkboxArray" ? [] : "";
  }
  return values;
}
