import { useReducer, useEffect, useCallback, useMemo } from "react";
import {
  TextInput,
  PasswordInput,
  Checkbox,
  CheckboxGroup,
  InlineNotification,
  InlineLoading,
  Dropdown,
  Section,
  Heading,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
  Button,
} from "@carbon/react";
import { SidePanel } from "@carbon/ibm-products";
import { ErrorFilled, Information } from "@carbon/icons-react";
import {
  ACTION_TYPES,
  INITIAL_STATE,
  addDataSourceModalReducer,
} from "./types";
import type { AddDataSourceModalProps } from "./types";
import { createDataSourceConnector } from "@/api/connectors.api";
import {
  parseConnectorSchema,
  groupFieldsBySections,
  buildInitialValues,
  type ConnectorField,
} from "./schemaUtils";
import { transformToCreateDatasourcePayload } from "./datasourceTransform";
import { useConnectorsStore } from "@/store/connectors.store";
import styles from "./AddDataSourceModal.module.scss";

/**
 * Field label with an optional Toggletip — clicking the info icon shows the field description.
 */
const FieldLabel = ({
  text,
  description,
}: {
  text: string;
  description?: string;
}) => (
  <div className={styles.labelWithInfo}>
    <span>{text}</span>
    {description && (
      <Toggletip align="top">
        <ToggletipButton label="Additional information">
          <Information />
        </ToggletipButton>
        <ToggletipContent>
          <p>{description}</p>
        </ToggletipContent>
      </Toggletip>
    )}
  </div>
);

const AddDataSourceModal = ({
  open,
  onClose,
  onSuccess,
}: AddDataSourceModalProps) => {
  const [state, dispatch] = useReducer(
    addDataSourceModalReducer,
    INITIAL_STATE,
  );
  // ── Read catalog data from store (pre-fetched by the table) ───────────────
  const {
    connectorTypes,
    connectorTypesLoading,
    connectorTypesError,
    getParams,
    isParamsLoading,
    paramsCacheError,
  } = useConnectorsStore();

  const {
    selectedType,
    showLocationOptionals,
    dataSourceName,
    formValues,
    nameInvalid,
    fieldErrors,
    isSubmitting,
    submitError,
  } = state;

  // ── Auto-select "Object Storage" type when types arrive and modal is open ─
  useEffect(() => {
    if (!open) return;
    if (connectorTypes.length > 0 && !selectedType) {
      const objectStorage = connectorTypes.find(
        (t) => t.provider.id === "object_storage",
      );
      dispatch({
        type: ACTION_TYPES.SET_SELECTED_TYPE,
        payload: objectStorage ?? connectorTypes[0],
      });
    }
  }, [open, connectorTypes, selectedType]);

  // ── Parse fields from schema ───────────────────────────────────────────────
  const paramsSchema = selectedType
    ? getParams(selectedType.provider.id)
    : null;

  const fields = useMemo(
    () => (paramsSchema ? parseConnectorSchema(paramsSchema) : []),
    [paramsSchema],
  );

  const sections = useMemo(() => groupFieldsBySections(fields), [fields]);

  // ── Sync form values when fields arrive ───────────────────────────────────
  useEffect(() => {
    if (fields.length === 0) {
      dispatch({ type: ACTION_TYPES.SET_FORM_VALUES, payload: {} });
      return;
    }
    dispatch({
      type: ACTION_TYPES.SET_FORM_VALUES,
      payload: buildInitialValues(fields),
    });
  }, [fields]);

  // ── Reset the whole form when modal closes ─────────────────────────────────
  const handleClose = useCallback(() => {
    dispatch({ type: ACTION_TYPES.RESET });
    onClose();
  }, [onClose]);

  // ── Field value helpers ────────────────────────────────────────────────────
  const setTextValue = (key: string, value: string) => {
    dispatch({ type: ACTION_TYPES.SET_TEXT_VALUE, payload: { key, value } });
  };

  const toggleCheckboxValue = (key: string, option: string) => {
    dispatch({
      type: ACTION_TYPES.TOGGLE_CHECKBOX_VALUE,
      payload: { key, option },
    });
  };

  // ── Derived loading / error states ────────────────────────────────────────
  const paramsLoading = selectedType
    ? isParamsLoading(selectedType.provider.id)
    : false;
  const paramsError = selectedType
    ? (paramsCacheError[selectedType.provider.id] ?? null)
    : null;

  // ── Validation ────────────────────────────────────────────────────────────
  const validateForm = (): boolean => {
    let valid = true;

    if (!dataSourceName.trim()) {
      valid = false;
    }

    const errors: Record<string, string> = {};
    for (const field of fields) {
      if (!field.isRequired) continue;
      const val = formValues[field.key];
      const isEmpty =
        field.type === "checkboxArray"
          ? (val as string[]).length === 0
          : !val || !String(val).trim();
      if (isEmpty) {
        errors[field.key] = `Provide a valid ${field.label.toLowerCase()}`;
        valid = false;
      }
    }
    dispatch({ type: ACTION_TYPES.SET_FIELD_ERRORS, payload: errors });

    return valid;
  };

  // ── Submit ─────────────────────────────────────────────────────────────────
  const handleSubmit = async () => {
    if (!validateForm() || !selectedType) return;

    dispatch({ type: ACTION_TYPES.SUBMIT_START });

    try {
      const payload = transformToCreateDatasourcePayload(
        dataSourceName.trim(),
        selectedType.provider.id,
        formValues,
        fields,
      );
      await createDataSourceConnector(payload);
      handleClose();
      onSuccess?.();
    } catch (err: unknown) {
      const serverMessage = (
        err as { response?: { data?: { error?: string } } }
      )?.response?.data?.error;
      dispatch({
        type: ACTION_TYPES.SUBMIT_FAILURE,
        payload:
          serverMessage ??
          (err instanceof Error ? err.message : "Failed to add data source"),
      });
    } finally {
      dispatch({ type: ACTION_TYPES.SUBMIT_END });
    }
  };

  // ── Field renderer ─────────────────────────────────────────────────────────
  const renderField = (field: ConnectorField) => {
    const fieldId = `add-datasource-${field.key}`;
    const fieldError = fieldErrors[field.key];
    const labelNode = (
      <FieldLabel text={field.label} description={field.description} />
    );

    switch (field.type) {
      case "checkboxArray": {
        const options = field.checkboxOptions ?? [];
        const selected = (formValues[field.key] as string[]) ?? [];
        const groupLabel = field.label;
        return (
          <CheckboxGroup
            key={field.key}
            legendText={
              <FieldLabel text={groupLabel} description={field.description} />
            }
            className={styles.checkboxGroup}
          >
            {options.map((option) => (
              <Checkbox
                key={option}
                id={`${fieldId}-${option}`}
                labelText={option}
                checked={selected.includes(option)}
                disabled={isSubmitting}
                onChange={() => toggleCheckboxValue(field.key, option)}
              />
            ))}
            {fieldError && (
              <p className={styles.checkboxError}>
                <ErrorFilled size={16} aria-hidden="true" />
                {fieldError}
              </p>
            )}
          </CheckboxGroup>
        );
      }

      case "password":
        return (
          <PasswordInput
            key={field.key}
            id={fieldId}
            labelText={labelNode}
            helperText=""
            invalid={!!fieldError}
            invalidText={fieldError}
            disabled={isSubmitting}
            value={(formValues[field.key] as string) ?? ""}
            onChange={(e) => setTextValue(field.key, e.target.value)}
          />
        );

      case "text":
        return (
          <TextInput
            key={field.key}
            id={fieldId}
            labelText={labelNode}
            invalid={!!fieldError}
            invalidText={fieldError}
            disabled={isSubmitting}
            value={(formValues[field.key] as string) ?? ""}
            onChange={(e) => setTextValue(field.key, e.target.value)}
          />
        );

      default: {
        // Exhaustiveness check — if ConnectorField["type"] gains a new
        // member without a corresponding case above, this line fails to
        // compile instead of silently falling back to a TextInput.
        const _exhaustive: never = field.type;
        return _exhaustive;
      }
    }
  };

  return (
    <SidePanel
      open={open}
      title="Add data source"
      size="md"
      placement="right"
      includeOverlay
      preventCloseOnClickOutside
      onRequestClose={handleClose}
      actions={[
        {
          label: isSubmitting ? "Adding data source..." : "Add",
          onClick: () => void handleSubmit(),
          kind: "primary",
          disabled: isSubmitting || paramsLoading,
          loading: isSubmitting,
        },
        {
          label: "Cancel",
          onClick: handleClose,
          kind: "secondary",
        },
      ]}
      className={styles.addDataSourcePanel}
    >
      <div className={styles.panelBody}>
        {/* ── Submit error ─────────────────────────────────────────────────── */}
        {submitError && (
          <InlineNotification
            kind="error"
            title="Error"
            subtitle={submitError}
            lowContrast
            hideCloseButton={false}
            onCloseButtonClick={() =>
              dispatch({ type: ACTION_TYPES.CLEAR_SUBMIT_ERROR })
            }
          />
        )}

        {/* ── Details section ──────────────────────────────────────────────── */}
        <Section className={styles.detailsSection}>
          <Heading className={styles.sectionHeading}>Details</Heading>

          <TextInput
            id="add-datasource-name"
            labelText="Data source name"
            value={dataSourceName}
            invalid={nameInvalid}
            invalidText="Data source name is required"
            disabled={isSubmitting}
            onChange={(e) => {
              dispatch({
                type: ACTION_TYPES.SET_DATA_SOURCE_NAME,
                payload: e.target.value,
              });
            }}
          />

          {connectorTypesLoading ? (
            <InlineLoading description="Loading source types..." />
          ) : connectorTypesError ? (
            <InlineNotification
              kind="error"
              title="Error"
              subtitle={connectorTypesError}
              lowContrast
              hideCloseButton
            />
          ) : (
            <>
              <Dropdown
                id="add-datasource-type"
                titleText="Source type"
                label="Select source type"
                items={connectorTypes ?? []}
                itemToString={(item) => item?.provider.name ?? ""}
                selectedItem={selectedType}
                disabled={isSubmitting}
                onChange={({ selectedItem }) => {
                  if (selectedItem) {
                    dispatch({
                      type: ACTION_TYPES.SET_SELECTED_TYPE,
                      payload: selectedItem,
                    });
                  }
                }}
              />
              {selectedType?.provider.description && (
                <p className={styles.connectorDescription}>
                  {selectedType.provider.description}
                </p>
              )}
            </>
          )}
        </Section>

        {/* ── Dynamic param sections ───────────────────────────────────────── */}
        {paramsLoading && <InlineLoading description="Loading fields..." />}

        {paramsError && !paramsLoading && (
          <InlineNotification
            kind="error"
            title="Error"
            subtitle={paramsError}
            lowContrast
            hideCloseButton
          />
        )}

        {!paramsLoading &&
          !paramsError &&
          sections.map((section) => {
            const isLocationSection = section.title === "Location";
            const visibleFields = isLocationSection
              ? section.fields.filter(
                  (f) => !f.isOptional || showLocationOptionals,
                )
              : section.fields;
            const hiddenOptionals = isLocationSection
              ? section.fields.filter((f) => f.isOptional)
              : [];

            return (
              <Section key={section.title} className={styles.paramSection}>
                <Heading className={styles.sectionTitle}>
                  {section.title}
                </Heading>

                {section.title === "File filters" && (
                  <div className={styles.fileFiltersInfo}>
                    <InlineNotification
                      kind="info"
                      title="Need more precise control?"
                      subtitle="The AI service will use all files in this data source. If you need finer control, set up a separate data source with just the files you want."
                      lowContrast
                      hideCloseButton
                    />
                  </div>
                )}

                {visibleFields.map((field) => renderField(field))}

                {isLocationSection &&
                  hiddenOptionals.length > 0 &&
                  !showLocationOptionals && (
                    <Button
                      kind="tertiary"
                      size="sm"
                      className={styles.addPrefixButton}
                      disabled={isSubmitting}
                      onClick={() =>
                        dispatch({ type: ACTION_TYPES.SHOW_LOCATION_OPTIONALS })
                      }
                    >
                      Add prefix +
                    </Button>
                  )}
              </Section>
            );
          })}
      </div>
    </SidePanel>
  );
};

export default AddDataSourceModal;
