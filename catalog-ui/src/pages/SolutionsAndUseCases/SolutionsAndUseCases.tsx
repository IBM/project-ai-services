import { useReducer, useMemo } from "react";
import { AccordionItem, CheckboxGroup, Checkbox } from "@carbon/react";
import { SolutionCard, CatalogBrowseLayout } from "@/components";
import { ACTION_TYPES, INITIAL_STATE, pageReducer } from "./types";

const SolutionsAndUseCasesPage = () => {
  const [state, dispatch] = useReducer(pageReducer, INITIAL_STATE);

  const handleDomainChange = (checked: boolean, value: string) => {
    const newDomains = checked
      ? [...state.filters.domains, value]
      : state.filters.domains.filter((d) => d !== value);
    dispatch({
      type: ACTION_TYPES.SET_DOMAIN_FILTER,
      payload: newDomains,
    });
  };

  const handleAssetChange = (checked: boolean, value: string) => {
    const newAssets = checked
      ? [...state.filters.assets, value]
      : state.filters.assets.filter((a) => a !== value);
    dispatch({
      type: ACTION_TYPES.SET_ASSET_FILTER,
      payload: newAssets,
    });
  };

  const filteredItems = state.items.filter((item) => {
    const matchesSearch =
      state.search === "" ||
      item.title.toLowerCase().includes(state.search.toLowerCase()) ||
      item.description.toLowerCase().includes(state.search.toLowerCase());

    const matchesDomain =
      state.filters.domains.length === 0 ||
      (item.domain && state.filters.domains.includes(item.domain));

    return matchesSearch && matchesDomain;
  });

  const domainCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    state.items.forEach((item) => {
      if (item.domain) {
        counts[item.domain] = (counts[item.domain] || 0) + 1;
      }
    });
    return counts;
  }, [state.items]);

  const assetCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    return counts;
  }, []);

  const totalSelectedFilters =
    state.filters.domains.length + state.filters.assets.length;

  return (
    <CatalogBrowseLayout
      title="Solutions and use cases"
      subtitle="Pre-built AI demos from real-world use cases to help you envision how AI can solve common business problems."
      searchValue={state.search}
      onSearchChange={(value) =>
        dispatch({
          type: ACTION_TYPES.SET_SEARCH,
          payload: value,
        })
      }
      totalSelectedFilters={totalSelectedFilters}
      onClearFilters={() => dispatch({ type: ACTION_TYPES.CLEAR_FILTERS })}
      filterAccordions={
        <>
          <AccordionItem title="Domains" open>
            <CheckboxGroup legendText="">
              <Checkbox
                labelText={`IT Ops (${domainCounts["IT Ops"] || 0})`}
                id="domain-it-ops"
                checked={state.filters.domains.includes("IT Ops")}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "IT Ops")
                }
              />
              <Checkbox
                labelText={`Customer service (${domainCounts["Customer service"] || 0})`}
                id="domain-customer"
                checked={state.filters.domains.includes("Customer service")}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "Customer service")
                }
              />
              <Checkbox
                labelText={`Development operations (${domainCounts["Development operations"] || 0})`}
                id="domain-dev-ops"
                checked={state.filters.domains.includes(
                  "Development operations",
                )}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "Development operations")
                }
              />
              <Checkbox
                labelText={`Enterprise search (${domainCounts["Enterprise search"] || 0})`}
                id="domain-enterprise"
                checked={state.filters.domains.includes("Enterprise search")}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "Enterprise search")
                }
              />
              <Checkbox
                labelText={`Healthcare (${domainCounts["Healthcare"] || 0})`}
                id="domain-healthcare"
                checked={state.filters.domains.includes("Healthcare")}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "Healthcare")
                }
              />
              <Checkbox
                labelText={`Public (${domainCounts["Public"] || 0})`}
                id="domain-public"
                checked={state.filters.domains.includes("Public")}
                onChange={(_, { checked }) =>
                  handleDomainChange(checked, "Public")
                }
              />
            </CheckboxGroup>
          </AccordionItem>

          <AccordionItem title="Assets">
            <CheckboxGroup legendText="">
              <Checkbox
                labelText={`Video demos (${assetCounts["Video demos"] || 0})`}
                id="asset-video"
                checked={state.filters.assets.includes("Video demos")}
                onChange={(_, { checked }) =>
                  handleAssetChange(checked, "Video demos")
                }
              />
              <Checkbox
                labelText={`Interactive prototypes (${assetCounts["Interactive prototypes"] || 0})`}
                id="asset-interactive"
                checked={state.filters.assets.includes(
                  "Interactive prototypes",
                )}
                onChange={(_, { checked }) =>
                  handleAssetChange(checked, "Interactive prototypes")
                }
              />
              <Checkbox
                labelText={`Reference stories (${assetCounts["Reference stories"] || 0})`}
                id="asset-reference"
                checked={state.filters.assets.includes("Reference stories")}
                onChange={(_, { checked }) =>
                  handleAssetChange(checked, "Reference stories")
                }
              />
            </CheckboxGroup>
          </AccordionItem>

          <AccordionItem title="Architectures">
            <CheckboxGroup legendText="">
              <Checkbox
                labelText="Sales and current mgmt"
                id="ref-sales"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Digital assistant"
                id="ref-digital"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Digital assistant"
                id="ref-digital-2"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Fraud detection"
                id="ref-fraud"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Financing"
                id="ref-financing"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Recommender system"
                id="ref-recommender"
                checked={false}
                onChange={() => {}}
              />
            </CheckboxGroup>
          </AccordionItem>

          <AccordionItem title="Services">
            <CheckboxGroup legendText="">
              <Checkbox
                labelText="Data and content mgmt"
                id="service-data"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Deep process integration"
                id="service-deep"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Digital assistant"
                id="service-digital"
                checked={false}
                onChange={() => {}}
              />
              <Checkbox
                labelText="Fraud detection"
                id="service-fraud"
                checked={false}
                onChange={() => {}}
              />
            </CheckboxGroup>
          </AccordionItem>
        </>
      }
      results={filteredItems.map((item) => (
        <SolutionCard
          key={item.id}
          id={item.id}
          title={item.title}
          description={item.description}
          tags={item.tags}
          category={item.category || "Agriculture"}
          onExplore={(id) => console.log("Explore", id)}
        />
      ))}
      emptyMessage="No solutions found matching your criteria."
    />
  );
};

export default SolutionsAndUseCasesPage;
