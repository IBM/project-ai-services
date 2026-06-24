import { useMemo } from "react";
import {
  TextInput,
  Dropdown,
  TextArea,
  Checkbox,
  NumberInput,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
} from "@carbon/react";
import { Information } from "@carbon/icons-react";
import {
  parseSchema,
  type ParsedField,
  type JSONSchema,
} from "@/utils/schemaParser";
import type { ProviderSchema } from "@/services/deployment.api";
import styles from "../ServicesDeployFlow.module.scss";

interface DynamicSchemaFieldsProps {
  componentType: string;
  providerId: string;
  values: Record<string, unknown>;
  onChange: (updates: Record<string, unknown>) => void;
  providerParamsMap: Record<string, ProviderSchema>;
  hasValidationError?: boolean;
  className?: string;
  fieldErrors?: Record<string, string>;
}

export const DynamicSchemaFields: React.FC<DynamicSchemaFieldsProps> = ({
  componentType,
  providerId,
  values,
  onChange,
  providerParamsMap,
  hasValidationError = false,
  className: _className,
  fieldErrors = {},
}) => {
  // Parse schema to get field definitions
  const fields = useMemo(() => {
    const schema = providerParamsMap[providerId];
    if (!schema) return [];

    // Cast ProviderSchema to JSONSchema for parseSchema compatibility
    // Both types are structurally compatible for our use case
    const parsedFields = parseSchema(schema as unknown as JSONSchema);

    // Filter out the 'model' field as it's handled separately
    return parsedFields.filter((field) => field.key !== "model");
  }, [providerParamsMap, providerId]);

  // If no additional fields, don't render anything
  if (fields.length === 0) {
    return null;
  }

  const handleFieldChange = (key: string, value: unknown) => {
    onChange({
      ...values,
      [key]: value,
    });
  };

  const renderField = (field: ParsedField) => {
    const fieldId = `${componentType}-${providerId}-${field.key}`;
    const value = values[field.key];

    // Get validation error for this field from parent
    const fieldError = fieldErrors[field.key];
    const isInvalid = hasValidationError && !!fieldError;
    const invalidText = fieldError || `${field.label} is required`;

    // Label with optional info tooltip
    const labelWithInfo = field.description ? (
      <div className={styles.labelWithInfo}>
        <span>{field.label}</span>
        <Toggletip align="top">
          <ToggletipButton label="Additional information">
            <Information />
          </ToggletipButton>
          <ToggletipContent>
            <p>{field.description}</p>
          </ToggletipContent>
        </Toggletip>
      </div>
    ) : (
      field.label
    );

    switch (field.type) {
      case "password":
        return (
          <TextInput
            key={fieldId}
            id={fieldId}
            labelText={labelWithInfo}
            type="password"
            value={String(value || "")}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );

      case "textarea":
        return (
          <TextArea
            key={fieldId}
            id={fieldId}
            labelText={labelWithInfo}
            value={String(value || "")}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
            rows={4}
          />
        );

      case "number":
        return (
          <NumberInput
            key={fieldId}
            id={fieldId}
            label={labelWithInfo}
            value={Number(value || field.defaultValue || 0)}
            invalid={isInvalid}
            invalidText={invalidText}
            min={field.validation?.min}
            max={field.validation?.max}
            onChange={(_e, { value: numValue }) => {
              handleFieldChange(
                field.key,
                numValue ? Number(numValue) : undefined,
              );
            }}
          />
        );

      case "boolean":
        return (
          <Checkbox
            key={fieldId}
            id={fieldId}
            labelText={field.label}
            checked={Boolean(value || field.defaultValue || false)}
            onChange={(e) => handleFieldChange(field.key, e.target.checked)}
          />
        );

      case "dropdown": {
        if (!field.options || field.options.length === 0) {
          return null;
        }
        const selectedItem =
          field.options.find((opt) => opt.id === value) || null;
        return (
          <Dropdown
            key={fieldId}
            id={fieldId}
            titleText={labelWithInfo}
            label="Select an option"
            items={field.options}
            itemToString={(item) => (item ? item.text : "")}
            selectedItem={selectedItem}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={({ selectedItem: item }) => {
              if (item) {
                handleFieldChange(field.key, item.id);
              }
            }}
          />
        );
      }

      case "text":
      default:
        return (
          <TextInput
            key={fieldId}
            id={fieldId}
            labelText={labelWithInfo}
            value={String(value || "")}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );
    }
  };

  // Group fields into rows of two
  const fieldRows: ParsedField[][] = [];
  for (let i = 0; i < fields.length; i += 2) {
    fieldRows.push(fields.slice(i, i + 2));
  }

  return (
    <>
      {fieldRows.map((row, rowIndex) => (
        <div key={`row-${rowIndex}`} className={styles.serviceConfigFieldRow}>
          {row.map((field) => (
            <div key={field.key} className={styles.serviceConfigFieldHalf}>
              {renderField(field)}
            </div>
          ))}
        </div>
      ))}
    </>
  );
};

// Made with Bob
