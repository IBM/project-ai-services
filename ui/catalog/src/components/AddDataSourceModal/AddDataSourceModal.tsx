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
  Button,
} from "@carbon/react";
import { SidePanel } from "@carbon/ibm-products";
import { ErrorFilled } from "@carbon/icons-react";
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
import { parseMessageCheckType } from "@/components/ConnectorDetailsPanel/types";
import styles from "./AddDataSourceModal.module.scss";

import ConnectorFieldLabel from "./ConnectorFieldLabel";

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
    sectionErrors,
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
  const setTextValue = (key: string, value: string, sectionTitle: string) => {
    dispatch({
      type: ACTION_TYPES.SET_TEXT_VALUE,
      payload: { key, value, sectionTitle },
    });
  };

  const toggleCheckboxValue = (
    key: string,
    option: string,
    sectionTitle: string,
  ) => {
    dispatch({
      type: ACTION_TYPES.TOGGLE_CHECKBOX_VALUE,
      payload: { key, option, sectionTitle },
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

    const fieldErrors: Record<string, string> = {};
    for (const field of fields) {
      if (!field.isRequired) continue;
      const val = formValues[field.key];
      const isEmpty =
        field.type === "checkboxArray"
          ? (val as string[]).length === 0
          : !val || !String(val).trim();
      if (isEmpty) {
        fieldErrors[field.key] = `Provide a valid ${field.label.toLowerCase()}`;
        valid = false;
      }
    }

    dispatch({
      type: ACTION_TYPES.SET_FIELD_ERRORS,
      payload: { fieldErrors, sectionErrors: {} },
    });

    return valid;
  };

  // Maps a server error message to { fieldErrors, sectionErrors } for inline
  // display, or null when nothing matched (caller shows a top-level banner).
  //
  // Steps:
  //   1. Extract [AUTH]/[ACCESS] prefix → limits word-scan to that section's
  //      fields so "host" in an AUTH message can't bleed into Location.
  //   2. Per field: try exact  at '/key':  match (schema errors), then
  //      \bkey\b word scan (connection-test errors, prefix-section only).
  //   3. No field hit + prefix present → section banner only.
  //      [ACCESS] also pins an inline error on the primary resource field
  //      (bucket_name / remote_path) since those messages never name the key.
  //   4. Nothing matched → return null.
  const parseServerError = (
    message: string,
  ): {
    fieldErrors: Record<string, string>;
    sectionErrors: Record<string, string>;
  } | null => {
    const fieldErrors: Record<string, string> = {};
    const sectionErrors: Record<string, string> = {};

    // Step 1 — extract prefix; maps to the section that owns it.
    const prefixSectionMap: Record<string, string> = {
      auth: "Authentication",
      access: "Location",
    };
    const inner = message.replace(/^Connection test failed:\s*/i, "");
    const { checkType, strippedMessage } = parseMessageCheckType(inner);
    const prefixSection = checkType ? prefixSectionMap[checkType] : undefined;

    // Step 2 — scan fields: exact at '/key': match first, then \bkey\b
    //          word scan restricted to the prefix section (if any).
    const structuredRe = /at\s+'\/([^']+)':/g;

    for (const field of fields) {
      let matched = false;

      // A: exact key from JSON Schema output — no section restriction needed.
      structuredRe.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = structuredRe.exec(message)) !== null) {
        if (m[1] === field.key) {
          matched = true;
          break;
        }
      }

      // B: word scan — only within the prefix section to avoid cross-section hits.
      if (
        !matched &&
        (!prefixSection || field.sectionTitle === prefixSection)
      ) {
        matched = new RegExp(`\\b${field.key}\\b`).test(message);
      }

      if (matched) {
        fieldErrors[field.key] =
          `Provide a valid ${field.label.toLowerCase()}.`;
        sectionErrors[field.sectionTitle] = strippedMessage;
      }
    }

    if (Object.keys(fieldErrors).length > 0) {
      return { fieldErrors, sectionErrors };
    }

    // Step 3 — no field hit: section banner only.
    //          [ACCESS] additionally pins the primary resource field inline.
    if (prefixSection) {
      const accessFieldByProvider: Record<string, string> = {
        object_storage: "bucket_name",
        file_system: "remote_path",
      };
      const accessTargetKey =
        checkType === "access" && selectedType
          ? accessFieldByProvider[selectedType.provider.id]
          : undefined;
      const accessTargetField = accessTargetKey
        ? fields.find((f) => f.key === accessTargetKey)
        : undefined;

      return {
        fieldErrors: accessTargetField
          ? {
              [accessTargetField.key]: `Provide a valid ${accessTargetField.label.toLowerCase()}.`,
            }
          : {},
        sectionErrors: { [prefixSection]: strippedMessage },
      };
    }

    // Step 4 — nothing matched: caller shows top-level banner.
    return null;
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

      if (serverMessage) {
        // [NETWORK] has no matching section — top-level banner.
        const inner = serverMessage.replace(/^Connection test failed:\s*/i, "");
        const { checkType, strippedMessage } = parseMessageCheckType(inner);
        if (checkType === "network") {
          dispatch({
            type: ACTION_TYPES.SUBMIT_FAILURE,
            payload: strippedMessage,
          });
          return;
        }

        // Route to field/section errors if possible.
        const parsed = parseServerError(serverMessage);
        if (parsed) {
          dispatch({ type: ACTION_TYPES.SET_FIELD_ERRORS, payload: parsed });
          return;
        }
      }

      // Unclassified error (name conflict, 500, etc.) — top-level banner.
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
      <ConnectorFieldLabel text={field.label} description={field.description} />
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
              <ConnectorFieldLabel
                text={groupLabel}
                description={field.description}
              />
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
                onChange={() =>
                  toggleCheckboxValue(field.key, option, field.sectionTitle)
                }
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
            onChange={(e) =>
              setTextValue(field.key, e.target.value, field.sectionTitle)
            }
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
            onChange={(e) =>
              setTextValue(field.key, e.target.value, field.sectionTitle)
            }
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

            const sectionError = sectionErrors[section.title];

            return (
              <Section key={section.title} className={styles.paramSection}>
                <Heading className={styles.sectionTitle}>
                  {section.title}
                </Heading>

                {sectionError && (
                  <InlineNotification
                    kind="error"
                    title="Error"
                    subtitle={sectionError}
                    lowContrast
                    hideCloseButton
                    className={styles.sectionErrorNotification}
                  />
                )}

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
