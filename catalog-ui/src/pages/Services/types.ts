export interface CatalogItem {
  id: string;
  title: string;
  description: string;
  tags: string[];
  category?: string;
  provider?: string;
  isCertified?: boolean;
}

export interface FilterState {
  providers: string[];
  referenceArchitectures: string[];
}

export interface PageState {
  search: string;
  items: CatalogItem[];
  filters: FilterState;
}

export const ACTION_TYPES = {
  SET_SEARCH: "SET_SEARCH",
  SET_PROVIDER_FILTER: "SET_PROVIDER_FILTER",
  SET_REFERENCE_ARCHITECTURE_FILTER: "SET_REFERENCE_ARCHITECTURE_FILTER",
  CLEAR_FILTERS: "CLEAR_FILTERS",
} as const;

export type PageAction =
  | { type: typeof ACTION_TYPES.SET_SEARCH; payload: string }
  | { type: typeof ACTION_TYPES.SET_PROVIDER_FILTER; payload: string[] }
  | {
      type: typeof ACTION_TYPES.SET_REFERENCE_ARCHITECTURE_FILTER;
      payload: string[];
    }
  | { type: typeof ACTION_TYPES.CLEAR_FILTERS };

// Mock data
export const MOCK_ITEMS: CatalogItem[] = [
  {
    id: "1",
    title: "Digitize documents",
    description:
      "Transforms documents such as manuals, invoices, and forms into text.",
    tags: ["Digital assistant", "Find similar items"],
    provider: "IBM",
    isCertified: true,
  },
  {
    id: "2",
    title: "Find similar items",
    description:
      "Fetches similar items from the system's knowledge management for a given input text or file.",
    tags: ["Digital assistant"],
    provider: "IBM",
    isCertified: true,
  },
  {
    id: "3",
    title: "Question and answer",
    description:
      "Answers questions in natural language by sourcing generic & domain-specific knowledge.",
    tags: ["Digital assistant", "Deep process integration"],
    provider: "IBM",
    isCertified: true,
  },
  {
    id: "4",
    title: "Summarization",
    description:
      "Consolidates a longer input text into a brief statement or account of the main points.",
    tags: ["Digital assistant", "Deep process integration"],
    provider: "IBM",
    isCertified: false,
  },
];

// Initial state
export const INITIAL_STATE: PageState = {
  search: "",
  items: MOCK_ITEMS,
  filters: {
    providers: [],
    referenceArchitectures: [],
  },
};

// Reducer
export const pageReducer = (
  state: PageState,
  action: PageAction,
): PageState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SEARCH:
      return { ...state, search: action.payload };
    case ACTION_TYPES.SET_PROVIDER_FILTER:
      return {
        ...state,
        filters: { ...state.filters, providers: action.payload },
      };
    case ACTION_TYPES.SET_REFERENCE_ARCHITECTURE_FILTER:
      return {
        ...state,
        filters: {
          ...state.filters,
          referenceArchitectures: action.payload,
        },
      };
    case ACTION_TYPES.CLEAR_FILTERS:
      return {
        ...state,
        filters: { providers: [], referenceArchitectures: [] },
      };
    default:
      return state;
  }
};
