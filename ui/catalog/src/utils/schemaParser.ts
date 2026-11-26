import type { JSONSchema, SchemaProperty } from "@/types/api.types";

export interface ParsedField {
  key: string;
  label: string;
  description?: string;
  type: "text" | "password" | "number" | "boolean" | "dropdown" | "textarea";
  defaultValue?: unknown;
  options?: Array<{ id: string; text: string; description?: string }>;
  validation?: {
    required: boolean;
    pattern?: string;
    format?: string;
    minLength?: number;
    maxLength?: number;
    min?: number;
    max?: number;
  };
  uiOnly?: boolean;
  controls?: string;
  controlledBy?: string;
}

// Parses a JSON Schema into a flat list of field definitions, recursively handling nested objects
export function parseSchema(schema: JSONSchema): ParsedField[] {
  if (!schema.properties) {
    return [];
  }

  const fields: ParsedField[] = [];
  const requiredFields = new Set(schema.required || []);

  for (const [key, property] of Object.entries(schema.properties)) {
    if (property === undefined) continue;
    if (property.type === "object" && property.properties) {
      // Recursively parse nested properties
      const nestedSchema: JSONSchema = {
        type: "object",
        properties: property.properties,
        required: property.required as string[] | undefined,
      };
      const nestedFields = parseSchema(nestedSchema);
      fields.push(...nestedFields);
    } else {
      // Regular field
      const field = parseSchemaProperty(key, property, requiredFields.has(key));
      if (field) {
        fields.push(field);
      }
    }
  }

  return fields;
}

// Parses a single schema property into a field definition, returning null for unsupported types
function parseSchemaProperty(
  key: string,
  property: SchemaProperty,
  isRequired: boolean,
): ParsedField | null {
  // Determine field type based on schema
  let fieldType: ParsedField["type"] = "text";
  let options: ParsedField["options"] | undefined;

  // Handle enum or oneOf (dropdown)
  if (property.enum) {
    fieldType = "dropdown";
    options = property.enum.map((value) => ({
      id: String(value),
      text: String(value),
    }));
  } else if (property.oneOf && Array.isArray(property.oneOf)) {
    fieldType = "dropdown";
    options = property.oneOf.map((option) => ({
      id: option.const,
      text: option.title || option.const,
      description: option.description,
    }));
  }
  // Handle format-based types
  else if (property.format === "password") {
    fieldType = "password";
  } else if (property.format === "textarea" || property.type === "textarea") {
    fieldType = "textarea";
  }
  // Automatically render long text fields as textareas
  else if (
    property.type === "string" &&
    property.maxLength &&
    property.maxLength >= 500
  ) {
    fieldType = "textarea";
  }
  // Handle basic types
  else if (property.type === "boolean") {
    fieldType = "boolean";
  } else if (property.type === "number" || property.type === "integer") {
    fieldType = "number";
  } else if (property.type === "string") {
    fieldType = "text";
  }

  // Build validation rules
  const validation: ParsedField["validation"] = {
    required: isRequired,
  };

  if (property.pattern) {
    validation.pattern = property.pattern;
  }
  if (property.format === "uri") {
    validation.format = property.format;
  }
  if (property.minLength !== undefined) {
    validation.minLength = property.minLength;
  }
  if (property.maxLength !== undefined) {
    validation.maxLength = property.maxLength;
  }
  if (property.minimum !== undefined) {
    validation.min = property.minimum;
  }
  if (property.maximum !== undefined) {
    validation.max = property.maximum;
  }

  return {
    key,
    label: property.title || formatLabel(key),
    description: property.description,
    type: fieldType,
    defaultValue: property.default,
    options,
    validation,
    uiOnly: property["x-ui-only"],
    controls: property["x-ui-controls"],
    controlledBy: property["x-ui-controlled-by"],
  };
}

// Converts a camelCase or snake_case key into a human-readable label
function formatLabel(key: string): string {
  return key
    .replace(/_/g, " ")
    .replace(/([A-Z])/g, " $1")
    .replace(/^./, (str) => str.toUpperCase())
    .trim();
}

// Checks for an absolute http(s) URL
function isHttpUrl(value: string): boolean {
  try {
    const { protocol } = new URL(value.trim());
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

// Patterns are written for Go and may not compile in JS; skip the check then and let the server validate
function matchesPattern(pattern: string, value: string): boolean {
  try {
    return new RegExp(pattern).test(value);
  } catch {
    return true;
  }
}

// Lowercases the first letter mid-sentence, keeping acronyms like "API"
function lowerFirst(label: string): string {
  return /^[A-Z][A-Z]/.test(label)
    ? label
    : label.charAt(0).toLowerCase() + label.slice(1);
}

// Validates a value against a parsed field's validation rules, returning an error string or null
export function validateField(
  value: unknown,
  field: ParsedField,
): string | null {
  if (!field.validation) {
    return null;
  }

  const { required, pattern, format, minLength, maxLength, min, max } =
    field.validation;

  // Required check
  if (
    required &&
    (value === undefined ||
      value === null ||
      (typeof value === "string" && value.trim() === ""))
  ) {
    return `Provide a valid ${lowerFirst(field.label)}`;
  }

  // Skip other validations if value is empty and not required
  if (
    !required &&
    (value === undefined ||
      value === null ||
      (typeof value === "string" && value.trim() === ""))
  ) {
    return null;
  }

  const stringValue = String(value);

  // Pattern validation
  if (pattern && !matchesPattern(pattern, stringValue)) {
    return `Provide a valid ${lowerFirst(field.label)}`;
  }

  // URI validation
  if (format === "uri" && !isHttpUrl(stringValue)) {
    return `Provide a valid ${lowerFirst(field.label)}`;
  }

  // Length validation
  if (minLength !== undefined && stringValue.length < minLength) {
    return `${field.label} must be at least ${minLength} characters`;
  }
  if (maxLength !== undefined && stringValue.length > maxLength) {
    return `${field.label} must be at most ${maxLength} characters`;
  }

  // Number validation
  if (field.type === "number") {
    const numValue = Number(value);
    if (isNaN(numValue)) {
      return `${field.label} must be a number`;
    }
    const belowMin = min !== undefined && numValue < min;
    const aboveMax = max !== undefined && numValue > max;
    if (belowMin || aboveMax) {
      if (min !== undefined && max !== undefined) {
        return `Provide a value between ${min}-${max}`;
      }
      return belowMin
        ? `Provide a value of at least ${min}`
        : `Provide a value of at most ${max}`;
    }
  }

  return null;
}

// Returns the default value for a field, falling back to type-appropriate defaults
export function getFieldDefault(field: ParsedField): unknown {
  if (field.defaultValue !== undefined) {
    return field.defaultValue;
  }

  // Return appropriate default based on type
  switch (field.type) {
    case "boolean":
      return false;
    case "number":
      return 0;
    case "dropdown":
      return field.options?.[0]?.id || "";
    default:
      return "";
  }
}
