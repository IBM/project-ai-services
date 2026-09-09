import type { DataSourceDetailResponse } from "@/types/api.types";
import type { ConnectorField } from "@/components/AddDataSourceModal/schemaUtils";
export type { ConnectorField };

export type DetailsPanelMode = "view" | "update-key";

/** How the authentication section is displayed. */
export type AuthSectionState =
  | { kind: "button" } // read-only view — shows "Update key" button
  | { kind: "form" } // editing — shows form fields
  | { kind: "saving" } // in-flight PATCH request
  | { kind: "success" } // PATCH succeeded — transient state
  | { kind: "error"; message: string }; // PATCH failed with inline error

export interface ConnectorDetailsPanelState {
  /** Latest loaded detail from GET /datasources/:id */
  detail: DataSourceDetailResponse | null;
  isLoading: boolean;
  fetchError: string | null;
  /** Current values in the authentication form */
  authFormValues: Record<string, string>;
  /** Validation errors keyed by field key */
  authFieldErrors: Record<string, string>;
  authSectionState: AuthSectionState;
}

export const PANEL_ACTION_TYPES = {
  FETCH_START: "FETCH_START",
  FETCH_SUCCESS: "FETCH_SUCCESS",
  FETCH_FAILURE: "FETCH_FAILURE",
  SHOW_AUTH_FORM: "SHOW_AUTH_FORM",
  HIDE_AUTH_FORM: "HIDE_AUTH_FORM",
  SET_AUTH_FIELD: "SET_AUTH_FIELD",
  SET_AUTH_FIELD_ERRORS: "SET_AUTH_FIELD_ERRORS",
  SAVE_START: "SAVE_START",
  SAVE_SUCCESS: "SAVE_SUCCESS",
  SAVE_FAILURE: "SAVE_FAILURE",
  CLEAR_AUTH_ERROR: "CLEAR_AUTH_ERROR",
  RESET: "RESET",
} as const;

export type PanelAction =
  | { type: typeof PANEL_ACTION_TYPES.FETCH_START }
  | {
      type: typeof PANEL_ACTION_TYPES.FETCH_SUCCESS;
      payload: DataSourceDetailResponse;
    }
  | { type: typeof PANEL_ACTION_TYPES.FETCH_FAILURE; payload: string }
  | {
      type: typeof PANEL_ACTION_TYPES.SHOW_AUTH_FORM;
      payload: Record<string, string>;
    }
  | { type: typeof PANEL_ACTION_TYPES.HIDE_AUTH_FORM }
  | {
      type: typeof PANEL_ACTION_TYPES.SET_AUTH_FIELD;
      payload: { key: string; value: string };
    }
  | {
      type: typeof PANEL_ACTION_TYPES.SET_AUTH_FIELD_ERRORS;
      payload: Record<string, string>;
    }
  | { type: typeof PANEL_ACTION_TYPES.SAVE_START }
  | { type: typeof PANEL_ACTION_TYPES.SAVE_SUCCESS }
  | {
      type: typeof PANEL_ACTION_TYPES.SAVE_FAILURE;
      payload: string;
      fieldErrors?: Record<string, string>;
    }
  | { type: typeof PANEL_ACTION_TYPES.CLEAR_AUTH_ERROR }
  | { type: typeof PANEL_ACTION_TYPES.RESET };

export const INITIAL_PANEL_STATE: ConnectorDetailsPanelState = {
  detail: null,
  isLoading: false,
  fetchError: null,
  authFormValues: {},
  authFieldErrors: {},
  authSectionState: { kind: "button" },
};

export function panelReducer(
  state: ConnectorDetailsPanelState,
  action: PanelAction,
): ConnectorDetailsPanelState {
  switch (action.type) {
    case PANEL_ACTION_TYPES.FETCH_START:
      return { ...state, isLoading: true, fetchError: null };
    case PANEL_ACTION_TYPES.FETCH_SUCCESS:
      return {
        ...state,
        isLoading: false,
        detail: action.payload,
        fetchError: null,
      };
    case PANEL_ACTION_TYPES.FETCH_FAILURE:
      return { ...state, isLoading: false, fetchError: action.payload };
    case PANEL_ACTION_TYPES.SHOW_AUTH_FORM:
      return {
        ...state,
        authFormValues: action.payload,
        authFieldErrors: {},
        authSectionState: { kind: "form" },
      };
    case PANEL_ACTION_TYPES.HIDE_AUTH_FORM:
      return {
        ...state,
        authFormValues: {},
        authFieldErrors: {},
        authSectionState: { kind: "button" },
      };
    case PANEL_ACTION_TYPES.SET_AUTH_FIELD: {
      const nextErrors = { ...state.authFieldErrors };
      delete nextErrors[action.payload.key];
      return {
        ...state,
        authFormValues: {
          ...state.authFormValues,
          [action.payload.key]: action.payload.value,
        },
        authFieldErrors: nextErrors,
      };
    }
    case PANEL_ACTION_TYPES.SET_AUTH_FIELD_ERRORS:
      return { ...state, authFieldErrors: action.payload };
    case PANEL_ACTION_TYPES.SAVE_START:
      return { ...state, authSectionState: { kind: "saving" } };
    case PANEL_ACTION_TYPES.SAVE_SUCCESS:
      return {
        ...state,
        authSectionState: { kind: "success" },
        authFormValues: {},
        authFieldErrors: {},
      };
    case PANEL_ACTION_TYPES.SAVE_FAILURE:
      return {
        ...state,
        authSectionState: { kind: "error", message: action.payload },
        authFieldErrors: action.fieldErrors ?? state.authFieldErrors,
      };
    case PANEL_ACTION_TYPES.CLEAR_AUTH_ERROR:
      return {
        ...state,
        authSectionState: { kind: "form" },
        authFieldErrors: {},
      };
    case PANEL_ACTION_TYPES.RESET:
      return INITIAL_PANEL_STATE;
    default:
      return state;
  }
}

/** The Connection section is derived from connector metadata + provider id. */
export interface ConnectionField {
  key: string;
  label: string;
  value: string;
}

/**
 * Returns the single Connection-section field label and value for the given
 * provider and metadata.  The label is taken from the schema field title so
 * it stays in sync with the provider definition.  Falls back to any key
 * ending in "_url", then "host", then the first metadata key.
 *
 * @param providerId  Provider id (e.g. "object_storage", "file_system")
 * @param metadata    Raw metadata from DataSourceDetailResponse
 * @param schemaFields Parsed ConnectorField list for this provider (may be [])
 */
export function deriveConnectionField(
  providerId: string,
  metadata: Record<string, unknown>,
  schemaFields: ConnectorField[],
): ConnectionField | null {
  // Provider-specific preferred key mappings.
  const CONNECTION_FIELD_MAP: Record<string, string> = {
    object_storage: "endpoint_url",
    file_system: "host",
  };

  const fieldKey =
    CONNECTION_FIELD_MAP[providerId] ??
    // Generic fallback order: *_url, host, first metadata key
    Object.keys(metadata).find((k) => k.endsWith("_url")) ??
    (Object.prototype.hasOwnProperty.call(metadata, "host") ? "host" : null) ??
    Object.keys(metadata)[0];

  if (!fieldKey) return null;

  const value = metadata[fieldKey];
  if (!value) return null;

  // Use the schema title so the label is always in sync with the provider definition.
  const schemaField = schemaFields.find((f) => f.key === fieldKey);
  const label = schemaField?.label ?? fieldKey;

  return { key: fieldKey, label, value: String(value) };
}
