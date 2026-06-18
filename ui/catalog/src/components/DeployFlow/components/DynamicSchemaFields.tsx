import { useMemo, useState } from "react";
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
import styles from "../DeployFlow.module.scss";

interface DynamicSchemaFieldsProps {
  componentType: string;
  providerId: string;
  values: Record<string, unknown>;
  onChange: (updates: Record<string, unknown>) => void;
  providerParamsMap: Record<string, JSONSchema>;
  hasValidationError?: boolean;
}

export const DynamicSchemaFields: React.FC<DynamicSchemaFieldsProps> = ({
  componentType,
  providerId,
  values,
  onChange,
  providerParamsMap,
  hasValidationError = false,
}) => {
  // Parse schema to get field definitions
  const fields = useMemo(() => {
    const schema = providerParamsMap[providerId];
    if (!schema) return [];

    const parsedFields = parseSchema(schema);

    // Filter out the 'model' field as it's handled separately
    return parsedFields.filter((field) => field.key !== "model");
  }, [providerParamsMap, providerId]);

  // State to track UI-only field values (checkboxes that control other fields)
  const [uiOnlyValues, setUiOnlyValues] = useState<Record<string, boolean>>({});

  // Compute UI-only values based on current values
  const computedUiOnlyValues = useMemo(() => {
    const computed: Record<string, boolean> = {};

    fields.forEach((field) => {
      if (field.uiOnly && field.controls) {
        // Check if the controlled field has a non-default value
        const controlledField = fields.find((f) => f.key === field.controls);
        const currentValue = values[field.controls];
        const isCustomized =
          currentValue !== undefined &&
          currentValue !== null &&
          currentValue !== controlledField?.defaultValue;

        // Use explicit state if set, otherwise use computed value
        computed[field.key] =
          uiOnlyValues[field.key] !== undefined
            ? uiOnlyValues[field.key]
            : isCustomized;
      }
    });

    return computed;
  }, [fields, values, uiOnlyValues]);

  // If no additional fields, don't render anything
  if (fields.length === 0) {
    return null;
  }

  const handleFieldChange = (key: string, value: unknown) => {
    // Filter out UI-only fields from the onChange callback
    const updatedValues = { ...values, [key]: value };

    // Remove UI-only fields before calling onChange
    const filteredValues: Record<string, unknown> = {};
    Object.entries(updatedValues).forEach(([k, v]) => {
      const field = fields.find((f) => f.key === k);
      if (!field?.uiOnly) {
        filteredValues[k] = v;
      }
    });

    onChange(filteredValues);
  };

  const handleUiOnlyChange = (
    key: string,
    checked: boolean,
    controlledFieldKey?: string,
  ) => {
    setUiOnlyValues((prev) => ({ ...prev, [key]: checked }));

    if (controlledFieldKey) {
      const controlledField = fields.find((f) => f.key === controlledFieldKey);

      if (checked) {
        // Checkbox checked: populate controlled field with default value
        const defaultValue = controlledField?.defaultValue || "";
        handleFieldChange(controlledFieldKey, defaultValue);
      } else {
        // Checkbox unchecked: remove controlled field value (will use default)
        const updatedValues = { ...values };
        delete updatedValues[controlledFieldKey];

        // Filter out UI-only fields
        const filteredValues: Record<string, unknown> = {};
        Object.entries(updatedValues).forEach(([k, v]) => {
          const field = fields.find((f) => f.key === k);
          if (!field?.uiOnly) {
            filteredValues[k] = v;
          }
        });

        onChange(filteredValues);
      }
    }
  };

  const renderField = (field: ParsedField) => {
    // Skip controlled fields if their controlling checkbox is unchecked
    if (field.controlledBy) {
      const controllingField = fields.find((f) => f.key === field.controlledBy);
      if (controllingField && !computedUiOnlyValues[field.controlledBy]) {
        return null; // Don't render if controlling checkbox is unchecked
      }
    }

    const fieldId = `${componentType}-${providerId}-${field.key}`;
    const value = values[field.key];
    const isInvalid =
      hasValidationError && field.validation?.required && !value;

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

    // Handle UI-only checkboxes (that control other fields)
    if (field.uiOnly && field.type === "boolean") {
      return (
        <Checkbox
          key={fieldId}
          id={fieldId}
          labelText={field.label}
          checked={computedUiOnlyValues[field.key] || false}
          onChange={(e) =>
            handleUiOnlyChange(field.key, e.target.checked, field.controls)
          }
        />
      );
    }

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
            invalidText={`${field.label} is required`}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );

      case "textarea":
        // Controlled textareas should span full width
        if (field.controlledBy) {
          return (
            <div
              key={fieldId}
              className={styles.systemPromptTextArea}
              style={{ gridColumn: "1 / -1" }}
            >
              <TextArea
                id={fieldId}
                labelText={labelWithInfo}
                value={String(value || "")}
                invalid={isInvalid}
                invalidText={`${field.label} is required`}
                onChange={(e) => handleFieldChange(field.key, e.target.value)}
                rows={4}
                maxCount={field.validation?.maxLength}
                enableCounter={!!field.validation?.maxLength}
              />
            </div>
          );
        }
        // Regular textareas
        return (
          <TextArea
            key={fieldId}
            id={fieldId}
            labelText={labelWithInfo}
            value={String(value || "")}
            invalid={isInvalid}
            invalidText={`${field.label} is required`}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
            rows={4}
            maxCount={field.validation?.maxLength}
            enableCounter={!!field.validation?.maxLength}
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
            invalidText={`${field.label} is required`}
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
            label={`Select ${field.label.toLowerCase()}`}
            items={field.options}
            itemToString={(item) => (item ? item.text : "")}
            selectedItem={selectedItem}
            invalid={isInvalid}
            invalidText={`${field.label} is required`}
            onChange={({ selectedItem }) =>
              handleFieldChange(field.key, selectedItem?.id || "")
            }
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
            invalidText={`${field.label} is required`}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );
    }
  };

  return (
    <>
      {/* Only show header for watsonx IBM provider */}
      {providerId.toLowerCase().includes("watsonx") && (
        <h4 className={styles.cloudCredentialsTitle}>Cloud credentials</h4>
      )}
      <div className={styles.dynamicSchemaFields}>
        {fields.map((field) => renderField(field))}
      </div>
    </>
  );
};
