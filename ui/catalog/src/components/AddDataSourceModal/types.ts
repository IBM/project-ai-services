import type { ConnectorType } from "@/types/api.types";

export type {
  ConnectorType,
  ParamProperty,
  ConnectorParamsSchema,
  CreateDatasourceRequest,
  CreateDatasourceResponse,
} from "@/types/api.types";

/** Internal form values keyed by param name */
export type FormValues = Record<string, string | string[]>;

export interface AddDataSourceModalProps {
  open: boolean;
  onClose: () => void;
  /** Called after a successful add — parent can trigger a table refresh */
  onSuccess?: () => void;
}

export interface AddDataSourceModalState {
  selectedType: ConnectorType | null;
  showLocationOptionals: boolean;
  dataSourceName: string;
  formValues: FormValues;
  nameInvalid: boolean;
  fieldErrors: Record<string, string>;
  isSubmitting: boolean;
  submitError: string | null;
}

export const ACTION_TYPES = {
  SET_SELECTED_TYPE: "SET_SELECTED_TYPE",
  SHOW_LOCATION_OPTIONALS: "SHOW_LOCATION_OPTIONALS",
  SET_DATA_SOURCE_NAME: "SET_DATA_SOURCE_NAME",
  SET_FORM_VALUES: "SET_FORM_VALUES",
  SET_TEXT_VALUE: "SET_TEXT_VALUE",
  TOGGLE_CHECKBOX_VALUE: "TOGGLE_CHECKBOX_VALUE",
  SET_FIELD_ERRORS: "SET_FIELD_ERRORS",
  CLEAR_SUBMIT_ERROR: "CLEAR_SUBMIT_ERROR",
  SUBMIT_START: "SUBMIT_START",
  SUBMIT_FAILURE: "SUBMIT_FAILURE",
  SUBMIT_END: "SUBMIT_END",
  RESET: "RESET",
} as const;

export type AddDataSourceModalAction =
  | {
      type: typeof ACTION_TYPES.SET_SELECTED_TYPE;
      payload: ConnectorType | null;
    }
  | { type: typeof ACTION_TYPES.SHOW_LOCATION_OPTIONALS }
  | { type: typeof ACTION_TYPES.SET_DATA_SOURCE_NAME; payload: string }
  | { type: typeof ACTION_TYPES.SET_FORM_VALUES; payload: FormValues }
  | {
      type: typeof ACTION_TYPES.SET_TEXT_VALUE;
      payload: { key: string; value: string };
    }
  | {
      type: typeof ACTION_TYPES.TOGGLE_CHECKBOX_VALUE;
      payload: { key: string; option: string };
    }
  | {
      type: typeof ACTION_TYPES.SET_FIELD_ERRORS;
      payload: Record<string, string>;
    }
  | { type: typeof ACTION_TYPES.CLEAR_SUBMIT_ERROR }
  | { type: typeof ACTION_TYPES.SUBMIT_START }
  | { type: typeof ACTION_TYPES.SUBMIT_FAILURE; payload: string }
  | { type: typeof ACTION_TYPES.SUBMIT_END }
  | { type: typeof ACTION_TYPES.RESET };

export const INITIAL_STATE: AddDataSourceModalState = {
  selectedType: null,
  showLocationOptionals: false,
  dataSourceName: "",
  formValues: {},
  nameInvalid: false,
  fieldErrors: {},
  isSubmitting: false,
  submitError: null,
};

export const addDataSourceModalReducer = (
  state: AddDataSourceModalState,
  action: AddDataSourceModalAction,
): AddDataSourceModalState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SELECTED_TYPE:
      return { ...state, selectedType: action.payload };
    case ACTION_TYPES.SHOW_LOCATION_OPTIONALS:
      return { ...state, showLocationOptionals: true };
    case ACTION_TYPES.SET_DATA_SOURCE_NAME:
      return {
        ...state,
        dataSourceName: action.payload,
        nameInvalid:
          state.nameInvalid && action.payload.trim()
            ? false
            : state.nameInvalid,
      };
    case ACTION_TYPES.SET_FORM_VALUES:
      return { ...state, formValues: action.payload };
    case ACTION_TYPES.SET_TEXT_VALUE: {
      const nextFieldErrors = { ...state.fieldErrors };
      delete nextFieldErrors[action.payload.key];
      return {
        ...state,
        formValues: {
          ...state.formValues,
          [action.payload.key]: action.payload.value,
        },
        fieldErrors: nextFieldErrors,
      };
    }
    case ACTION_TYPES.TOGGLE_CHECKBOX_VALUE: {
      const current = (state.formValues[action.payload.key] as string[]) ?? [];
      const next = current.includes(action.payload.option)
        ? current.filter((value) => value !== action.payload.option)
        : [...current, action.payload.option];
      const nextFieldErrors = { ...state.fieldErrors };
      if (next.length > 0) {
        delete nextFieldErrors[action.payload.key];
      }
      return {
        ...state,
        formValues: { ...state.formValues, [action.payload.key]: next },
        fieldErrors: nextFieldErrors,
      };
    }
    case ACTION_TYPES.SET_FIELD_ERRORS:
      return {
        ...state,
        fieldErrors: action.payload,
        nameInvalid: !state.dataSourceName.trim(),
      };
    case ACTION_TYPES.CLEAR_SUBMIT_ERROR:
      return { ...state, submitError: null };
    case ACTION_TYPES.SUBMIT_START:
      return { ...state, isSubmitting: true, submitError: null };
    case ACTION_TYPES.SUBMIT_FAILURE:
      return { ...state, submitError: action.payload };
    case ACTION_TYPES.SUBMIT_END:
      return { ...state, isSubmitting: false };
    case ACTION_TYPES.RESET:
      return INITIAL_STATE;
    default:
      return state;
  }
};
