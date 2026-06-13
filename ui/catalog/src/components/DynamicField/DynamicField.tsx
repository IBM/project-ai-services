import React from "react";
import {
  TextInput,
  Dropdown,
  Checkbox,
  TextArea,
  NumberInput,
} from "@carbon/react";
import type { ParsedField } from "@/utils/schemaParser";

interface DynamicFieldProps {
  field: ParsedField;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled?: boolean;
  invalid?: boolean;
  invalidText?: string;
}

/**
 * DynamicField Component
 * Renders the appropriate Carbon component based on field type from JSON Schema
 */
export const DynamicField: React.FC<DynamicFieldProps> = ({
  field,
  value,
  onChange,
  disabled = false,
  invalid = false,
  invalidText,
}) => {
  const { key, label, description, type, options } = field;

  switch (type) {
    case "dropdown": {
      if (!options || options.length === 0) {
        return null;
      }

      const selectedItem = options.find((opt) => opt.id === value) || null;

      return (
        <Dropdown
          id={key}
          titleText={label}
          label={`Select ${label.toLowerCase()}`}
          helperText={description}
          items={options}
          itemToString={(item) => (item ? item.text : "")}
          selectedItem={selectedItem}
          onChange={({ selectedItem }) => onChange(selectedItem?.id || "")}
          disabled={disabled}
          invalid={invalid}
          invalidText={invalidText || `${label} is required`}
        />
      );
    }

    case "boolean":
      return (
        <Checkbox
          id={key}
          labelText={label}
          checked={Boolean(value)}
          onChange={(e) => onChange(e.target.checked)}
          disabled={disabled}
        />
      );

    case "number":
      return (
        <NumberInput
          id={key}
          label={label}
          helperText={description}
          value={Number(value) || 0}
          onChange={(_e, { value: newValue }) => {
            onChange(newValue === "" ? 0 : Number(newValue));
          }}
          disabled={disabled}
          invalid={invalid}
          invalidText={invalidText || `${label} is required`}
          min={field.validation?.min}
          max={field.validation?.max}
        />
      );

    case "textarea":
      return (
        <TextArea
          id={key}
          labelText={label}
          helperText={description}
          value={String(value || "")}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          invalid={invalid}
          invalidText={invalidText || `${label} is required`}
          rows={4}
          maxCount={field.validation?.maxLength}
          enableCounter={!!field.validation?.maxLength}
        />
      );

    case "password":
      return (
        <TextInput
          id={key}
          labelText={label}
          helperText={description}
          type="password"
          value={String(value || "")}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          invalid={invalid}
          invalidText={invalidText || `${label} is required`}
          maxLength={field.validation?.maxLength}
        />
      );

    case "text":
    default:
      return (
        <TextInput
          id={key}
          labelText={label}
          helperText={description}
          value={String(value || "")}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          invalid={invalid}
          invalidText={invalidText || `${label} is required`}
          maxLength={field.validation?.maxLength}
        />
      );
  }
};
