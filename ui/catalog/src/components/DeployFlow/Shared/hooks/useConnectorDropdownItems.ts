import { useReducer, useEffect } from "react";
import { fetchAllDataSourceConnectors } from "@/api/connectors.api";
import type { DropdownItem } from "../types";

function toDropdownItem(connector: {
  id: string;
  name: string;
  provider: { name: string };
}): DropdownItem {
  return {
    id: connector.id,
    label: `${connector.name} (${connector.provider.name})`,
  };
}

interface State {
  items: DropdownItem[];
  loading: boolean;
  fetchError: string | null;
}

type Action =
  | { type: "RESET" }
  | { type: "FETCH_START" }
  | { type: "FETCH_SUCCESS"; payload: DropdownItem[] }
  | { type: "FETCH_ERROR"; payload: string };

const initialState: State = { items: [], loading: false, fetchError: null };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "RESET":
      return initialState;
    case "FETCH_START":
      return { items: [], loading: true, fetchError: null };
    case "FETCH_SUCCESS":
      return { items: action.payload, loading: false, fetchError: null };
    case "FETCH_ERROR":
      return { ...state, loading: false, fetchError: action.payload };
    default:
      return state;
  }
}

export interface UseConnectorDropdownItemsResult {
  items: DropdownItem[];
  loading: boolean;
  fetchError: string | null;
}

/**
 * Fetches the list of data source connectors when `enabled` flips to true.
 * Re-fetches every time `enabled` transitions from false → true, giving the
 * caller fresh data on each toggle-on. Cancels any in-flight request and
 * resets state when `enabled` flips back to false.
 */
export function useConnectorDropdownItems(
  enabled: boolean,
): UseConnectorDropdownItemsResult {
  const [state, dispatch] = useReducer(reducer, initialState);

  useEffect(() => {
    if (!enabled) {
      dispatch({ type: "RESET" });
      return;
    }

    let cancelled = false;
    dispatch({ type: "FETCH_START" });

    fetchAllDataSourceConnectors()
      .then((connectors) => {
        if (!cancelled) {
          dispatch({
            type: "FETCH_SUCCESS",
            payload: connectors.map(toDropdownItem),
          });
        }
      })
      .catch((err) => {
        if (!cancelled) {
          dispatch({
            type: "FETCH_ERROR",
            payload:
              err instanceof Error
                ? err.message
                : "Failed to load data sources",
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [enabled]);

  return {
    items: enabled ? state.items : [],
    loading: enabled ? state.loading : false,
    fetchError: enabled ? state.fetchError : null,
  };
}
