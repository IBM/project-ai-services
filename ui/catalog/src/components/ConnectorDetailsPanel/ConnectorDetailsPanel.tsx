import React, { useReducer, useEffect, useCallback, useMemo } from "react";
import { SidePanel } from "@carbon/ibm-products";
import {
  Button,
  Link,
  PasswordInput,
  TextInput,
  InlineNotification,
  InlineLoading,
  SkeletonText,
  Section,
  Heading,
  Tag,
  Stack,
  DataTable,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableContainer,
} from "@carbon/react";
import { NoDataEmptyState } from "@carbon/ibm-products";
import {
  panelReducer,
  INITIAL_PANEL_STATE,
  PANEL_ACTION_TYPES,
  deriveConnectionField,
  parseMessageCheckType,
} from "./types";
import type { DetailsPanelMode } from "./types";
import {
  fetchDataSourceById,
  updateDataSourceAuth,
} from "@/api/connectors.api";
import { useConnectorsStore } from "@/store/connectors.store";
import {
  parseConnectorSchema,
  groupFieldsBySections,
  buildInitialValues,
} from "@/components/AddDataSourceModal/schemaUtils";
import type { ConnectorField } from "@/components/AddDataSourceModal/schemaUtils";
import ConnectorFieldLabel from "@/components/AddDataSourceModal/ConnectorFieldLabel";
import { normalizePrivateKey } from "@/components/AddDataSourceModal/datasourceTransform";
import { calculateUptime } from "@/utils/time";
import styles from "./ConnectorDetailsPanel.module.scss";
import { STATUS_CONFIG } from "@/components/Table/components/CellRenderers";
import sharedStyles from "@/components/Table/table.shared.module.scss";

export interface ConnectorDetailsPanelProps {
  open: boolean;
  connectorId: string | null;
  /** Determines initial auth section state */
  mode: DetailsPanelMode;
  onClose: () => void;
}

const SUCCESS_DISPLAY_MS = 2000;

// Order in which sections are rendered in the panel
const SECTION_ORDER = [
  "Connection",
  "Location",
  "Authentication",
  "Services",
] as const;

const SERVICES_HEADERS = [
  { key: "name", header: "Name" },
  { key: "last_sync", header: "Last sync" },
];

const ConnectorDetailsPanel = ({
  open,
  connectorId,
  mode,
  onClose,
}: ConnectorDetailsPanelProps) => {
  const [state, dispatch] = useReducer(panelReducer, INITIAL_PANEL_STATE);
  const paramsCache = useConnectorsStore((s) => s.paramsCache);

  // ── Load detail on open ────────────────────────────────────────────────────
  useEffect(() => {
    if (!open || !connectorId) {
      dispatch({ type: PANEL_ACTION_TYPES.RESET });
      return;
    }

    let cancelled = false;

    dispatch({ type: PANEL_ACTION_TYPES.FETCH_START });

    fetchDataSourceById(connectorId)
      .then((data) => {
        if (!cancelled) {
          dispatch({ type: PANEL_ACTION_TYPES.FETCH_SUCCESS, payload: data });
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const msg =
            err instanceof Error
              ? err.message
              : "Failed to load connector details";
          dispatch({ type: PANEL_ACTION_TYPES.FETCH_FAILURE, payload: msg });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, connectorId]);

  // ── Derive fields from params schema ──────────────────────────────────────
  const paramsSchema = useMemo(
    () =>
      state.detail
        ? (paramsCache[state.detail.provider.id]?.data ?? null)
        : null,
    [state.detail, paramsCache],
  );

  const allFields: ConnectorField[] = useMemo(
    () => (paramsSchema ? parseConnectorSchema(paramsSchema) : []),
    [paramsSchema],
  );

  const sectionedFields = useMemo(
    () => groupFieldsBySections(allFields),
    [allFields],
  );

  const authFields: ConnectorField[] = useMemo(
    () =>
      sectionedFields.find((s) => s.title === "Authentication")?.fields ?? [],
    [sectionedFields],
  );

  // ── Honour mode prop once detail + fields are ready ───────────────────────
  useEffect(() => {
    if (!state.detail?.id || authFields.length === 0) return;

    if (mode === "update-key") {
      dispatch({
        type: PANEL_ACTION_TYPES.SHOW_AUTH_FORM,
        payload: buildInitialValues(authFields) as Record<string, string>,
      });
    }
    // "view" mode: INITIAL_PANEL_STATE already shows the button
  }, [state.detail?.id, authFields, mode]);

  // ── Success auto-dismiss ───────────────────────────────────────────────────
  useEffect(() => {
    if (state.authSectionState.kind !== "success") return;
    const timer = setTimeout(() => {
      dispatch({ type: PANEL_ACTION_TYPES.HIDE_AUTH_FORM });
    }, SUCCESS_DISPLAY_MS);
    return () => clearTimeout(timer);
  }, [state.authSectionState.kind]);

  // ── Handlers ──────────────────────────────────────────────────────────────
  const handleClose = useCallback(() => {
    dispatch({ type: PANEL_ACTION_TYPES.RESET });
    onClose();
  }, [onClose]);

  const handleShowAuthForm = useCallback(() => {
    if (!state.detail) return;
    dispatch({
      type: PANEL_ACTION_TYPES.SHOW_AUTH_FORM,
      payload: buildInitialValues(authFields) as Record<string, string>,
    });
  }, [state.detail, authFields]);

  const handleFieldChange = useCallback((key: string, value: string) => {
    dispatch({
      type: PANEL_ACTION_TYPES.SET_AUTH_FIELD,
      payload: { key, value },
    });
  }, []);

  const handleCancel = useCallback(() => {
    dispatch({ type: PANEL_ACTION_TYPES.HIDE_AUTH_FORM });
  }, []);

  const handleSave = useCallback(async () => {
    if (!connectorId) return;

    // Validate required auth fields
    const errors: Record<string, string> = {};
    for (const field of authFields) {
      if (!field.isRequired) continue;
      const val = (state.authFormValues[field.key] ?? "").trim();
      if (!val) {
        errors[field.key] = `Provide a valid ${field.label.toLowerCase()}`;
      }
    }
    if (Object.keys(errors).length > 0) {
      dispatch({
        type: PANEL_ACTION_TYPES.SET_AUTH_FIELD_ERRORS,
        payload: errors,
      });
      return;
    }

    dispatch({ type: PANEL_ACTION_TYPES.SAVE_START });

    try {
      const params: Record<string, string | string[]> = {};
      for (const field of authFields) {
        let val = (state.authFormValues[field.key] ?? "").trim();
        if (field.key === "private_key") {
          val = normalizePrivateKey(val);
        }
        if (val) params[field.key] = val;
      }
      await updateDataSourceAuth(connectorId, { params });
      dispatch({ type: PANEL_ACTION_TYPES.SAVE_SUCCESS });
    } catch (err: unknown) {
      const serverMessage = (
        err as { response?: { data?: { error?: string } } }
      )?.response?.data?.error;

      // Mark every auth field as invalid and surface the banner error atomically.
      const fieldErrors: Record<string, string> = {};
      for (const field of authFields) {
        fieldErrors[field.key] = `Provide a valid ${field.label.toLowerCase()}`;
      }
      dispatch({
        type: PANEL_ACTION_TYPES.SAVE_FAILURE,
        payload:
          serverMessage ??
          (err instanceof Error
            ? err.message
            : "Failed to update authentication key"),
        fieldErrors,
      });
    }
  }, [connectorId, authFields, state.authFormValues]);

  // ── Render helpers ─────────────────────────────────────────────────────────

  const renderReadOnlyField = (
    label: string,
    value: string,
    description?: string,
    key?: string,
  ) => (
    <div className={styles.field} key={key}>
      {description ? (
        <div className={styles.fieldLabel}>
          <ConnectorFieldLabel text={label} description={description} />
        </div>
      ) : (
        <div className={styles.fieldLabel}>{label}</div>
      )}
      <div className={styles.fieldValue}>{value}</div>
    </div>
  );

  const renderAuthField = (field: ConnectorField) => {
    const isSaving = state.authSectionState.kind === "saving";
    const fieldError = state.authFieldErrors[field.key];
    const value = state.authFormValues[field.key] ?? "";
    const id = `connector-auth-${field.key}`;
    const labelNode = (
      <ConnectorFieldLabel text={field.label} description={field.description} />
    );

    switch (field.type) {
      case "password":
        return (
          <PasswordInput
            key={field.key}
            id={id}
            labelText={labelNode}
            helperText=""
            value={value}
            invalid={!!fieldError}
            invalidText={fieldError}
            disabled={isSaving}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );

      case "text":
        return (
          <TextInput
            key={field.key}
            id={id}
            labelText={labelNode}
            value={value}
            invalid={!!fieldError}
            invalidText={fieldError}
            disabled={isSaving}
            onChange={(e) => handleFieldChange(field.key, e.target.value)}
          />
        );

      case "checkboxArray":
        // Checkbox fields don't appear in the auth section but are handled
        // here for exhaustiveness — auth section only renders password/text.
        return null;

      default: {
        const _exhaustive: never = field.type;
        return _exhaustive;
      }
    }
  };

  // ── Section renderers ──────────────────────────────────────────────────────

  // ── Parse the backend message prefix once — shared by all section renderers ─
  const { checkType: errorCheckType, strippedMessage: errorMessage } =
    parseMessageCheckType(state.detail?.message);

  const renderConnectionSection = () => {
    if (!state.detail) return null;
    const connectionField = deriveConnectionField(
      state.detail.provider.id,
      state.detail.metadata,
      allFields,
    );
    const { status } = state.detail;
    const statusCfg = STATUS_CONFIG[status as keyof typeof STATUS_CONFIG] ?? {
      tagType: "gray" as const,
      icon: undefined,
      className: sharedStyles.statusTagSecondary,
    };

    return (
      <Section level={3} className={styles.section}>
        <Heading className={styles.sectionTitle}>Connection</Heading>
        {/* Network error — e.g. endpoint unreachable */}
        {errorCheckType === "network" && (
          <InlineNotification
            kind="error"
            title="Endpoint unreachable"
            subtitle={errorMessage}
            lowContrast
            hideCloseButton
            className={styles.inlineNotification}
          />
        )}
        {connectionField &&
          renderReadOnlyField(connectionField.label, connectionField.value)}
        <Tag
          type={statusCfg.tagType}
          size="md"
          renderIcon={statusCfg.icon}
          className={`${styles.statusTag} ${statusCfg.className}`}
        >
          {status}
        </Tag>
      </Section>
    );
  };

  const renderAuthSection = () => {
    if (!state.detail) return null;
    const { authSectionState } = state;

    // Show the auth error notification only when the backend reported an auth failure,
    // OR when there is an unclassified message (no prefix) — preserves previous behaviour
    // for any connector type that does not yet emit a structured prefix.
    const hasAuthError =
      authSectionState.kind === "button" &&
      state.detail.status !== "connected" &&
      !!state.detail.message &&
      (errorCheckType === "auth" || errorCheckType === null);

    return (
      <Section level={3} className={styles.section}>
        <Heading className={styles.sectionTitle}>Authentication</Heading>

        {/* Invalid credentials notification — view mode only */}
        {hasAuthError && (
          <InlineNotification
            kind="error"
            title="Invalid key credentials"
            subtitle={errorMessage}
            lowContrast
            hideCloseButton
            className={styles.inlineNotification}
          />
        )}

        {/* PATCH error notification */}
        {authSectionState.kind === "error" && (
          <InlineNotification
            kind="error"
            title="Authentication failed"
            subtitle={authSectionState.message}
            lowContrast
            onClose={() =>
              dispatch({ type: PANEL_ACTION_TYPES.CLEAR_AUTH_ERROR })
            }
            className={styles.inlineNotification}
          />
        )}

        {/* View mode — "Update key" button; disabled when connector is offline */}
        {authSectionState.kind === "button" && (
          <Button
            kind="tertiary"
            size="sm"
            disabled={state.detail?.status === "offline"}
            onClick={handleShowAuthForm}
          >
            Update key
          </Button>
        )}

        {/* Edit / saving / success / error states — show form fields */}
        {authSectionState.kind !== "button" && (
          <>
            <Stack gap={5}>{authFields.map(renderAuthField)}</Stack>

            <Stack
              orientation="horizontal"
              gap={3}
              className={styles.authFormActions}
            >
              {authSectionState.kind === "saving" ? (
                <InlineLoading description="Authenticating..." />
              ) : authSectionState.kind === "success" ? (
                <InlineLoading
                  description="Authentication successful"
                  status="finished"
                />
              ) : (
                <>
                  <Button kind="secondary" size="sm" onClick={handleCancel}>
                    Cancel
                  </Button>
                  <Button
                    kind="primary"
                    size="sm"
                    onClick={() => void handleSave()}
                  >
                    Save
                  </Button>
                </>
              )}
            </Stack>
          </>
        )}
      </Section>
    );
  };

  const renderLocationSection = () => {
    if (!state.detail) return null;
    const locationFields =
      sectionedFields.find((s) => s.title === "Location")?.fields ?? [];
    if (locationFields.length === 0) return null;

    const connectionKey = deriveConnectionField(
      state.detail.provider.id,
      state.detail.metadata,
      allFields,
    )?.key;

    const visibleFields = locationFields.filter((field) => {
      const v = state.detail!.metadata[field.key];
      return (
        field.key !== connectionKey && v !== undefined && v !== null && v !== ""
      );
    });
    if (visibleFields.length === 0 && errorCheckType !== "access") return null;

    return (
      <Section level={3} className={styles.section}>
        <Heading className={styles.sectionTitle}>Location</Heading>
        {/* Access error — e.g. bucket not found */}
        {errorCheckType === "access" && (
          <InlineNotification
            kind="error"
            title="Resource not accessible"
            subtitle={errorMessage}
            lowContrast
            hideCloseButton
            className={styles.inlineNotification}
          />
        )}
        {visibleFields.map((field) =>
          renderReadOnlyField(
            field.label,
            String(state.detail!.metadata[field.key]),
            field.description,
            field.key,
          ),
        )}
      </Section>
    );
  };

  const renderServicesSection = () => {
    if (!state.detail) return null;
    const applications = state.detail.applications;

    // Build rows for Carbon DataTable
    // app_type is carried on each row for custom cell rendering;
    // it is not in SERVICES_HEADERS so it won't produce its own column.
    const rows = applications.map((app) => ({
      id: app.id,
      name: app.name,
      app_type: app.type,
      last_sync: app.last_sync_at
        ? `${calculateUptime(app.last_sync_at)} ago`
        : (app.sync_status ?? "—"),
    }));

    return (
      <Section level={3} className={styles.section}>
        <Stack orientation="horizontal" className={styles.servicesHeader}>
          <Heading className={styles.sectionTitle}>Services</Heading>
          <Tag type="gray" size="sm" className={styles.serviceTag}>
            {applications.length}
          </Tag>
        </Stack>

        {applications.length === 0 ? (
          <NoDataEmptyState
            title="No services"
            subtitle="This data source has no connected services."
          />
        ) : (
          <DataTable rows={rows} headers={SERVICES_HEADERS} size="sm">
            {({
              rows: tableRows,
              headers,
              getHeaderProps,
              getRowProps,
              getTableProps,
            }) => (
              <TableContainer>
                <Table {...getTableProps()} className={styles.servicesTable}>
                  <TableHead>
                    <TableRow>
                      {headers.map((header) => {
                        const { key, ...rest } = getHeaderProps({ header });
                        return (
                          <TableHeader
                            key={key}
                            {...rest}
                            className={
                              header.key === "last_sync"
                                ? styles.lastSyncCol
                                : undefined
                            }
                          >
                            {header.header}
                          </TableHeader>
                        );
                      })}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {tableRows.map((row) => {
                      const { key: rowKey, ...rowProps } = getRowProps({ row });
                      return (
                        <TableRow key={rowKey} {...rowProps}>
                          {row.cells.map((cell) => (
                            <TableCell
                              key={cell.id}
                              className={
                                cell.info.header === "last_sync"
                                  ? styles.lastSyncCol
                                  : undefined
                              }
                            >
                              {cell.info.header === "name" ? (
                                <Stack gap={1} className={styles.serviceName}>
                                  <Link href="#">{String(cell.value)}</Link>
                                  <span className={styles.serviceTypeName}>
                                    {
                                      rows.find((r) => r.id === row.id)
                                        ?.app_type
                                    }
                                  </span>
                                </Stack>
                              ) : (
                                cell.value
                              )}
                            </TableCell>
                          ))}
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </TableContainer>
            )}
          </DataTable>
        )}
      </Section>
    );
  };

  // ── Panel ──────────────────────────────────────────────────────────────────

  const title =
    state.detail?.name ?? (state.isLoading ? "" : "Connector details");
  const labelText = state.detail?.provider.name ?? "";

  const sectionRenderers = {
    Connection: renderConnectionSection,
    Location: renderLocationSection,
    Authentication: renderAuthSection,
    Services: renderServicesSection,
  };

  return (
    <SidePanel
      open={open}
      onRequestClose={handleClose}
      title={title}
      labelText={labelText}
      placement="right"
      size="sm"
      slideIn
      selectorPageContent="#connectors-page-content"
    >
      {state.isLoading && (
        <Stack gap={6} className={styles.skeletonContent}>
          <Stack gap={5}>
            <SkeletonText heading width="40%" />
            <SkeletonText lineCount={2} />
          </Stack>
          <hr className={styles.divider} />
          <Stack gap={5}>
            <SkeletonText heading width="40%" />
            <SkeletonText lineCount={2} />
          </Stack>
          <hr className={styles.divider} />
          <Stack gap={5}>
            <SkeletonText heading width="40%" />
            <SkeletonText lineCount={1} width="60%" />
          </Stack>
          <hr className={styles.divider} />
          <Stack gap={5}>
            <SkeletonText heading width="40%" />
            <SkeletonText lineCount={4} />
          </Stack>
        </Stack>
      )}

      {!state.isLoading && state.fetchError && (
        <Stack gap={6} className={styles.content}>
          <InlineNotification
            kind="error"
            title="Error loading connector"
            subtitle={state.fetchError}
            lowContrast
            hideCloseButton
          />
        </Stack>
      )}

      {!state.isLoading && !state.fetchError && state.detail && (
        <Stack gap={6} className={styles.content}>
          {SECTION_ORDER.map((sectionName, i) => {
            const render = sectionRenderers[sectionName];
            const node = render();
            if (!node) return null;
            return (
              <React.Fragment key={sectionName}>
                {node}
                {i < SECTION_ORDER.length - 1 && (
                  <hr className={styles.divider} />
                )}
              </React.Fragment>
            );
          })}
        </Stack>
      )}
    </SidePanel>
  );
};

export default ConnectorDetailsPanel;
