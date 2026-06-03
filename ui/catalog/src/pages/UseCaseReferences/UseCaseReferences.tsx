import { useReducer, useMemo } from "react";
import { AccordionItem, Checkbox } from "@carbon/react";
import CatalogBrowseLayout from "@/layouts/CatalogBrowseLayout";
import SolutionCard from "@/components/SolutionCard";
import SolutionDetailPanel from "@/components/SolutionDetailPanel";
import { useUseCases } from "@/hooks/useUseCases";
import { normalizeString } from "@/utils/string";

// State type definition
interface FilterState {
  searchValue: string;
  selectedProviders: string[];
  selectedDomains: string[];
  selectedAssets: string[];
  selectedArchitectures: string[];
  isPanelOpen: boolean;
  selectedSolutionId: string | null;
}

// Action types
type FilterAction =
  | { type: "SET_SEARCH"; payload: string }
  | { type: "TOGGLE_PROVIDER"; payload: { checked: boolean; value: string } }
  | { type: "TOGGLE_DOMAIN"; payload: { checked: boolean; value: string } }
  | { type: "TOGGLE_ASSET"; payload: { checked: boolean; value: string } }
  | {
      type: "TOGGLE_ARCHITECTURE";
      payload: { checked: boolean; value: string };
    }
  | { type: "CLEAR_FILTERS" }
  | { type: "OPEN_PANEL"; payload: string }
  | { type: "CLOSE_PANEL" };

// Initial state
const initialState: FilterState = {
  searchValue: "",
  selectedProviders: [],
  selectedDomains: [],
  selectedAssets: [],
  selectedArchitectures: [],
  isPanelOpen: false,
  selectedSolutionId: null,
};

// Reducer function
const filterReducer = (
  state: FilterState,
  action: FilterAction,
): FilterState => {
  switch (action.type) {
    case "SET_SEARCH":
      return { ...state, searchValue: action.payload };

    case "TOGGLE_PROVIDER":
      return {
        ...state,
        selectedProviders: action.payload.checked
          ? [...state.selectedProviders, action.payload.value]
          : state.selectedProviders.filter((p) => p !== action.payload.value),
      };

    case "TOGGLE_DOMAIN":
      return {
        ...state,
        selectedDomains: action.payload.checked
          ? [...state.selectedDomains, action.payload.value]
          : state.selectedDomains.filter((d) => d !== action.payload.value),
      };

    case "TOGGLE_ASSET":
      return {
        ...state,
        selectedAssets: action.payload.checked
          ? [...state.selectedAssets, action.payload.value]
          : state.selectedAssets.filter((a) => a !== action.payload.value),
      };

    case "TOGGLE_ARCHITECTURE":
      return {
        ...state,
        selectedArchitectures: action.payload.checked
          ? [...state.selectedArchitectures, action.payload.value]
          : state.selectedArchitectures.filter(
              (a) => a !== action.payload.value,
            ),
      };

    case "CLEAR_FILTERS":
      return {
        ...state,
        searchValue: "",
        selectedProviders: [],
        selectedDomains: [],
        selectedAssets: [],
        selectedArchitectures: [],
      };

    case "OPEN_PANEL":
      return {
        ...state,
        isPanelOpen: true,
        selectedSolutionId: action.payload,
      };

    case "CLOSE_PANEL":
      return {
        ...state,
        isPanelOpen: false,
        selectedSolutionId: null,
      };

    default:
      return state;
  }
};

const UseCaseReferences = () => {
  const { useCases: solutions, isLoading, error } = useUseCases();
  const [state, dispatch] = useReducer(filterReducer, initialState);

  const {
    searchValue,
    selectedProviders,
    selectedDomains,
    selectedAssets,
    selectedArchitectures,
    isPanelOpen,
    selectedSolutionId,
  } = state;

  const handleProviderChange = (checked: boolean, value: string) => {
    dispatch({ type: "TOGGLE_PROVIDER", payload: { checked, value } });
  };

  const handleDomainChange = (checked: boolean, value: string) => {
    dispatch({ type: "TOGGLE_DOMAIN", payload: { checked, value } });
  };

  const handleAssetChange = (checked: boolean, value: string) => {
    dispatch({ type: "TOGGLE_ASSET", payload: { checked, value } });
  };

  const handleArchitectureChange = (checked: boolean, value: string) => {
    dispatch({ type: "TOGGLE_ARCHITECTURE", payload: { checked, value } });
  };

  const handleClearFilters = () => {
    dispatch({ type: "CLEAR_FILTERS" });
  };

  const totalSelectedFilters =
    selectedProviders.length +
    selectedDomains.length +
    selectedAssets.length +
    selectedArchitectures.length;

  // Calculate dynamic counts
  const ibmCount = useMemo(() => {
    return solutions.filter((sol) => sol.creator === "IBM").length;
  }, [solutions]);

  const domainCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    solutions.forEach((sol) => {
      const key = normalizeString(sol.domain);
      counts[key] = (counts[key] || 0) + 1;
    });
    return counts;
  }, [solutions]);

  const assetCounts = useMemo(() => {
    const counts: Record<string, number> = {};

    solutions.forEach((sol) => {
      sol.assets.forEach((asset) => {
        const key = normalizeString(asset);
        counts[key] = (counts[key] || 0) + 1;
      });
    });

    return counts;
  }, [solutions]);

  const architectureCounts = useMemo(() => {
    const counts: Record<string, number> = {};

    solutions.forEach((sol) => {
      sol.architectures.forEach((arch) => {
        const key = normalizeString(arch);
        counts[key] = (counts[key] || 0) + 1;
      });
    });

    return counts;
  }, [solutions]);

  // Get unique assets and architectures dynamically
  const uniqueAssets = useMemo(() => {
    const assets = new Set<string>();
    solutions.forEach((sol) => {
      sol.assets.forEach((asset) => assets.add(asset));
    });
    return Array.from(assets).sort();
  }, [solutions]);

  const uniqueArchitectures = useMemo(() => {
    const architectures = new Set<string>();
    solutions.forEach((sol) => {
      sol.architectures.forEach((arch) => architectures.add(arch));
    });
    return Array.from(architectures).sort();
  }, [solutions]);

  // Filter solutions based on selected filters and search
  const filteredSolutions = useMemo(() => {
    return solutions.filter((sol) => {
      const matchesProvider =
        selectedProviders.length === 0 ||
        (selectedProviders.includes("ibm") && sol.creator === "IBM");

      const matchesDomain =
        selectedDomains.length === 0 ||
        selectedDomains.some((domain) => {
          return normalizeString(sol.domain) === domain;
        });

      const matchesAsset =
        selectedAssets.length === 0 ||
        sol.assets.some((asset) => {
          return selectedAssets.includes(normalizeString(asset));
        });

      const matchesArchitecture =
        selectedArchitectures.length === 0 ||
        sol.architectures.some((arch) => {
          return selectedArchitectures.includes(normalizeString(arch));
        });

      // Search in card content (title, description, domain, assets, architectures)
      const matchesSearch =
        !searchValue ||
        sol.title.toLowerCase().includes(searchValue.toLowerCase()) ||
        sol.description.toLowerCase().includes(searchValue.toLowerCase()) ||
        sol.domain.toLowerCase().includes(searchValue.toLowerCase()) ||
        sol.assets.some((asset) =>
          asset.toLowerCase().includes(searchValue.toLowerCase()),
        ) ||
        sol.architectures.some((arch) =>
          arch.toLowerCase().includes(searchValue.toLowerCase()),
        );

      return (
        matchesProvider &&
        matchesDomain &&
        matchesAsset &&
        matchesArchitecture &&
        matchesSearch
      );
    });
  }, [
    solutions,
    selectedProviders,
    selectedDomains,
    selectedAssets,
    selectedArchitectures,
    searchValue,
  ]);

  // Filter options
  const providerOptions = useMemo(() => {
    return [{ label: "IBM", value: "ibm", count: ibmCount }];
  }, [ibmCount]);

  const domainOptions = useMemo(() => {
    // Dynamically generate domain options from actual data
    const uniqueDomains = Array.from(
      new Set(solutions.map((sol) => sol.domain)),
    );

    return uniqueDomains
      .map((domain) => {
        const key = normalizeString(domain);
        return {
          label: domain,
          value: key,
          count: domainCounts[key] || 0,
        };
      })
      .sort((a, b) => a.label.localeCompare(b.label));
  }, [solutions, domainCounts]);

  const assetOptions = useMemo(() => {
    return uniqueAssets.map((asset) => {
      const key = normalizeString(asset);
      return {
        label: asset,
        value: key,
        count: assetCounts[key] || 0,
      };
    });
  }, [uniqueAssets, assetCounts]);

  const architectureOptions = useMemo(() => {
    return uniqueArchitectures.map((arch) => {
      const key = normalizeString(arch);
      return {
        label: arch,
        value: key,
        count: architectureCounts[key] || 0,
      };
    });
  }, [uniqueArchitectures, architectureCounts]);

  const filterAccordions = (
    <>
      {providerOptions.length > 0 && (
        <AccordionItem title="Provider" open>
          {providerOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`provider-${option.value}`}
              checked={selectedProviders.includes(option.value)}
              onChange={(_, { checked }) =>
                handleProviderChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {domainOptions.length > 0 && (
        <AccordionItem title="Domains" open>
          {domainOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`domain-${option.value}`}
              checked={selectedDomains.includes(option.value)}
              onChange={(_, { checked }) =>
                handleDomainChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {assetOptions.length > 0 && (
        <AccordionItem title="Assets" open>
          {assetOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`asset-${option.value}`}
              checked={selectedAssets.includes(option.value)}
              onChange={(_, { checked }) =>
                handleAssetChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}

      {architectureOptions.length > 0 && (
        <AccordionItem title="Architectures" open>
          {architectureOptions.map((option) => (
            <Checkbox
              key={option.value}
              labelText={`${option.label} (${option.count})`}
              id={`architecture-${option.value}`}
              checked={selectedArchitectures.includes(option.value)}
              onChange={(_, { checked }) =>
                handleArchitectureChange(checked, option.value)
              }
            />
          ))}
        </AccordionItem>
      )}
    </>
  );

  const results = isLoading ? (
    <div>Loading use cases...</div>
  ) : error ? (
    <div>Error loading use cases: {error}</div>
  ) : filteredSolutions.length > 0 ? (
    <>
      {filteredSolutions.map((sol) => (
        <SolutionCard
          key={sol.id}
          id={sol.id}
          title={sol.title}
          description={sol.description}
          tags={sol.assets}
          category={sol.domain}
          onViewDetails={(id) => {
            dispatch({ type: "OPEN_PANEL", payload: id });
          }}
        />
      ))}
    </>
  ) : null;

  return (
    <>
      <CatalogBrowseLayout
        title="Use case references"
        subtitle="Ready-to-explore use cases based on real-world AI solutions to help you envision how AI can solve common business problems—and accelerate your AI journey."
        searchValue={searchValue}
        onSearchChange={(value) =>
          dispatch({ type: "SET_SEARCH", payload: value })
        }
        totalSelectedFilters={totalSelectedFilters}
        onClearFilters={handleClearFilters}
        filterAccordions={filterAccordions}
        results={results}
        emptyMessage="No solutions match your filters. Try adjusting your search or clearing filters."
        showLearnMore={false}
      />
      <SolutionDetailPanel
        open={isPanelOpen}
        onClose={() => {
          dispatch({ type: "CLOSE_PANEL" });
        }}
        solutionId={selectedSolutionId}
      />
    </>
  );
};

export default UseCaseReferences;
