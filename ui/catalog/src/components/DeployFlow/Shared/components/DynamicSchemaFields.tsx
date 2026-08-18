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
import { parseSchema, type ParsedField } from "@/utils/schemaParser";
import type { JSONSchema } from "@/types/api.types";
import { parseMarkdownLinks } from "@/utils/string";
import styles from "../DeployFlow.shared.module.scss";

interface DynamicSchemaFieldsProps {
  componentType: string;
  providerId: string;
  values: Record<string, unknown>;
  onChange: (updates: Record<string, unknown>) => void;
  providerParamsMap: Record<string, JSONSchema>;
  hasValidationError?: boolean;
  fieldErrors?: Record<string, string>;
}

export const DynamicSchemaFields: React.FC<DynamicSchemaFieldsProps> = ({
  componentType,
  providerId,
  values,
  onChange,
  providerParamsMap,
  hasValidationError = false,
  fieldErrors = {},
}) => {
  const fields = useMemo(() => {
    const schema = providerParamsMap[providerId];
    if (!schema) return [];
    return parseSchema(schema).filter((field) => field.key !== "model");
  }, [providerParamsMap, providerId]);

  const [uiOnlyValues, setUiOnlyValues] = useState<Record<string, boolean>>({});

  const fieldsByKey = useMemo(
    () => Object.fromEntries(fields.map((f) => [f.key, f])),
    [fields],
  );

  const computedUiOnlyValues = useMemo(() => {
    const computed: Record<string, boolean> = {};
    fields.forEach((field) => {
      if (field.uiOnly && field.controls) {
        const controlledField = fieldsByKey[field.controls];
        const currentValue = values[field.controls];
        const isCustomized =
          currentValue !== undefined &&
          currentValue !== null &&
          currentValue !== controlledField?.defaultValue;
        computed[field.key] =
          uiOnlyValues[field.key] !== undefined
            ? uiOnlyValues[field.key]
            : isCustomized;
      }
    });
    return computed;
  }, [fields, fieldsByKey, values, uiOnlyValues]);

  if (fields.length === 0) {
    return null;
  }

  const handleFieldChange = (key: string, value: unknown) => {
    const updatedValues = { ...values, [key]: value };
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
      if (checked) {
        const controlledField = fields.find(
          (f) => f.key === controlledFieldKey,
        );
        handleFieldChange(
          controlledFieldKey,
          controlledField?.defaultValue || "",
        );
      } else {
        const updatedValues = { ...values };
        delete updatedValues[controlledFieldKey];
        const filteredValues: Record<string, unknown> = {};
        Object.entries(updatedValues).forEach(([k, v]) => {
          if (!fields.find((f) => f.key === k)?.uiOnly) {
            filteredValues[k] = v;
          }
        });
        onChange(filteredValues);
      }
    }
  };

  const renderField = (field: ParsedField) => {
    if (field.controlledBy) {
      const controllingField = fields.find((f) => f.key === field.controlledBy);
      if (controllingField && !computedUiOnlyValues[field.controlledBy]) {
        return null;
      }
    }

    const fieldId = `${componentType}-${providerId}-${field.key}`;
    const value = values[field.key];
    const fieldError = fieldErrors[field.key];
    const isInvalid = hasValidationError && !!fieldError;
    const invalidText = fieldError || `Provide a valid ${field.label}`;

    const labelWithInfo =
      field.description && field.key === "watsonxProjectId" ? (
        <div className={styles.labelWithInfo}>
          <span>{field.label}</span>
          <Toggletip align="top">
            <ToggletipButton label="Additional information">
              <Information />
            </ToggletipButton>
            <ToggletipContent>
              <p>{parseMarkdownLinks(field.description)}</p>
            </ToggletipContent>
          </Toggletip>
        </div>
      ) : (
        field.label
      );

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
            required={field.validation?.required}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );

      case "textarea":
        if (field.controlledBy) {
          return (
            <div key={fieldId} className={styles.systemPromptTextArea}>
              <TextArea
                id={fieldId}
                labelText={labelWithInfo}
                value={String(value || "")}
                invalid={isInvalid}
                invalidText={invalidText}
                onChange={(e) => handleFieldChange(field.key, e.target.value)}
                rows={4}
                maxCount={field.validation?.maxLength}
                enableCounter={!!field.validation?.maxLength}
              />
            </div>
          );
        }
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
            required={field.validation?.required}
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
            label={`Select ${field.label.toLowerCase()}`}
            items={field.options}
            itemToString={(item) => (item ? item.text : "")}
            selectedItem={selectedItem}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={({ selectedItem: item }) =>
              handleFieldChange(field.key, item?.id || "")
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
            required={field.validation?.required}
            invalid={isInvalid}
            invalidText={invalidText}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );
    }
  };

  return (
    <div className={styles.dynamicSchemaFields}>
      {fields.map((field) => renderField(field))}
    </div>
  );
};
