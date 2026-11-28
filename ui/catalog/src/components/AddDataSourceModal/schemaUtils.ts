import type { ConnectorParamsSchema, FormValues } from "./types";
export interface ConnectorField {
  key: string;
  label: string;
  description?: string;
  type: "text" | "password" | "checkboxArray" | "checkbox";
  checkboxOptions?: string[];
  sectionTitle: string;
  isRequired: boolean;
  isOptional: boolean;
  schemaType?: string;
  minimum?: number;
  maximum?: number;
  controlledBy?: string;
  hideWhenChecked?: boolean;
  requiredWhenVisible?: boolean;
  alwaysVisible?: boolean;
}

// Fields declaring x-ui-required-when-visible are mandatory whenever shown
export function isRequiredWhenVisible(field: ConnectorField): boolean {
  return field.isRequired || !!field.requiredWhenVisible;
}

export function isNumericField(field: ConnectorField): boolean {
  return field.schemaType === "integer" || field.schemaType === "number";
}

/**
 * Returns an error message when `text` is not a valid value for a numeric
 * field (plain decimal digits, within the schema's minimum/maximum), or null.
 */
export function getNumberError(
  field: ConnectorField,
  text: string,
): string | null {
  const isInteger = field.schemaType === "integer";
  const decimalPattern = isInteger ? /^-?\d+$/ : /^-?\d+(\.\d+)?$/;
  if (!decimalPattern.test(text)) {
    return `Provide a valid ${field.label.toLowerCase()}`;
  }

  const num = Number(text);
  if (isInteger && !Number.isSafeInteger(num)) {
    return `Provide a valid ${field.label.toLowerCase()}`;
  }

  const { minimum, maximum } = field;
  if (
    (minimum !== undefined && num < minimum) ||
    (maximum !== undefined && num > maximum)
  ) {
    if (minimum !== undefined && maximum !== undefined) {
      return `${field.label} must be between ${minimum} and ${maximum}`;
    }
    return minimum !== undefined && num < minimum
      ? `${field.label} must be at least ${minimum}`
      : `${field.label} must be at most ${maximum}`;
  }

  return null;
}

export function isFieldHidden(
  field: ConnectorField,
  formValues: FormValues,
): boolean {
  if (!field.controlledBy || !field.hideWhenChecked) return false;
  return formValues[field.controlledBy] === true;
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
    } else if (property.type === "boolean") {
      type = "checkbox";
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
      schemaType: property.type,
      minimum: property.minimum,
      maximum: property.maximum,
      controlledBy: property["x-ui-controlled-by"],
      hideWhenChecked: property["x-ui-hide-when-checked"],
      requiredWhenVisible: property["x-ui-required-when-visible"],
      alwaysVisible: property["x-ui-always-visible"],
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
 * pre-selecting all options for checkboxArray fields, unchecking checkbox
 * fields and setting all others to "".
 */
export function buildInitialValues(fields: ConnectorField[]): FormValues {
  const values: FormValues = {};
  for (const field of fields) {
    if (field.type === "checkboxArray") {
      values[field.key] = [...(field.checkboxOptions ?? [])];
    } else if (field.type === "checkbox") {
      values[field.key] = false;
    } else {
      values[field.key] = "";
    }
  }
  return values;
}
