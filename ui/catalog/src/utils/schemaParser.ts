/**
 * Schema Parser Utility
 * Parses JSON Schema to extract field definitions for dynamic form rendering
 */

export interface SchemaProperty {
  type: string;
  title?: string;
  description?: string;
  default?: unknown;
  format?: string;
  enum?: string[];
  oneOf?: Array<{ const: string; title: string; description?: string }>;
  pattern?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  [key: string]: unknown;
}

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
    minLength?: number;
    maxLength?: number;
    min?: number;
    max?: number;
  };
}

export interface JSONSchema {
  $schema?: string;
  type: string;
  properties?: Record<string, SchemaProperty>;
  required?: string[];
  [key: string]: unknown;
}

/**
 * Parses JSON Schema and extracts field definitions
 */
export function parseSchema(schema: JSONSchema): ParsedField[] {
  if (!schema.properties) {
    return [];
  }

  const fields: ParsedField[] = [];
  const requiredFields = new Set(schema.required || []);

  for (const [key, property] of Object.entries(schema.properties)) {
    const field = parseSchemaProperty(key, property, requiredFields.has(key));
    if (field) {
      fields.push(field);
    }
  }

  return fields;
}

/**
 * Parses a single schema property into a field definition
 */
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
  };
}

/**
 * Formats a camelCase or snake_case key into a readable label
 */
function formatLabel(key: string): string {
  return key
    .replace(/_/g, " ")
    .replace(/([A-Z])/g, " $1")
    .replace(/^./, (str) => str.toUpperCase())
    .trim();
}

/**
 * Validates a value against a parsed field's validation rules
 */
export function validateField(
  value: unknown,
  field: ParsedField,
): string | null {
  if (!field.validation) {
    return null;
  }

  const { required, pattern, minLength, maxLength, min, max } =
    field.validation;

  // Required check
  if (required && (value === undefined || value === null || value === "")) {
    return `${field.label} is required`;
  }

  // Skip other validations if value is empty and not required
  if (!required && (value === undefined || value === null || value === "")) {
    return null;
  }

  const stringValue = String(value);

  // Pattern validation
  if (pattern && !new RegExp(pattern).test(stringValue)) {
    return `${field.label} format is invalid`;
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
    if (min !== undefined && numValue < min) {
      return `${field.label} must be at least ${min}`;
    }
    if (max !== undefined && numValue > max) {
      return `${field.label} must be at most ${max}`;
    }
  }

  return null;
}

/**
 * Gets the default value for a field from schema
 */
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
