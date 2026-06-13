import type { ServiceDetailData } from "@/components";

// Action types
export const ACTION_TYPES = {
  SERVICES_SET_SELECTED_SERVICE: "SERVICES_SET_SELECTED_SERVICE",
  SERVICES_SET_PANEL_OPEN: "SERVICES_SET_PANEL_OPEN",
  SERVICES_SET_SERVICES: "SERVICES_SET_SERVICES",
  SERVICES_SET_LOADING: "SERVICES_SET_LOADING",
  SERVICES_SET_ERROR: "SERVICES_SET_ERROR",
  SERVICES_SET_MOCK_SERVICES: "SERVICES_SET_MOCK_SERVICES",
  SERVICES_SET_HAS_FETCHED_SERVICES: "SERVICES_SET_HAS_FETCHED_SERVICES",
  SERVICES_CLOSE_PANEL: "SERVICES_CLOSE_PANEL",
} as const;

// State interface
export interface ServicesState {
  selectedService: ServiceDetailData | null;
  isPanelOpen: boolean;
  services: ServiceDetailData[];
  loading: boolean;
  error: string | null;
  mockServices: ServiceDetailData[];
  hasFetchedServices: boolean;
}

// Initial state
export const INITIAL_STATE: ServicesState = {
  selectedService: null,
  isPanelOpen: false,
  services: [],
  loading: false,
  error: null,
  mockServices: [],
  hasFetchedServices: false,
};

// Action types
export type ServicesAction =
  | {
      type: typeof ACTION_TYPES.SERVICES_SET_SELECTED_SERVICE;
      payload: ServiceDetailData | null;
    }
  | { type: typeof ACTION_TYPES.SERVICES_SET_PANEL_OPEN; payload: boolean }
  | {
      type: typeof ACTION_TYPES.SERVICES_SET_SERVICES;
      payload: ServiceDetailData[];
    }
  | { type: typeof ACTION_TYPES.SERVICES_SET_LOADING; payload: boolean }
  | { type: typeof ACTION_TYPES.SERVICES_SET_ERROR; payload: string | null }
  | {
      type: typeof ACTION_TYPES.SERVICES_SET_MOCK_SERVICES;
      payload: ServiceDetailData[];
    }
  | {
      type: typeof ACTION_TYPES.SERVICES_SET_HAS_FETCHED_SERVICES;
      payload: boolean;
    }
  | { type: typeof ACTION_TYPES.SERVICES_CLOSE_PANEL };

// Reducer function
export const servicesReducer = (
  state: ServicesState,
  action: ServicesAction,
): ServicesState => {
  switch (action.type) {
    case ACTION_TYPES.SERVICES_SET_SELECTED_SERVICE:
      return { ...state, selectedService: action.payload };

    case ACTION_TYPES.SERVICES_SET_PANEL_OPEN:
      return { ...state, isPanelOpen: action.payload };

    case ACTION_TYPES.SERVICES_SET_SERVICES:
      return { ...state, services: action.payload };

    case ACTION_TYPES.SERVICES_SET_LOADING:
      return { ...state, loading: action.payload };

    case ACTION_TYPES.SERVICES_SET_ERROR:
      return { ...state, error: action.payload };

    case ACTION_TYPES.SERVICES_SET_MOCK_SERVICES:
      return { ...state, mockServices: action.payload };

    case ACTION_TYPES.SERVICES_SET_HAS_FETCHED_SERVICES:
      return { ...state, hasFetchedServices: action.payload };

    case ACTION_TYPES.SERVICES_CLOSE_PANEL:
      return { ...state, isPanelOpen: false };

    default:
      return state;
  }
};

// Made with Bob
