import { useReducer, useMemo } from "react";
import { AccordionItem, CheckboxGroup, Checkbox } from "@carbon/react";
import { CatalogCard, CatalogBrowseLayout } from "@/components";
import { ACTION_TYPES, INITIAL_STATE, pageReducer } from "./types";

const ServicesPage = () => {
  const [state, dispatch] = useReducer(pageReducer, INITIAL_STATE);

  const handleProviderChange = (checked: boolean, value: string) => {
    const newProviders = checked
      ? [...state.filters.providers, value]
      : state.filters.providers.filter((p) => p !== value);
    dispatch({
      type: ACTION_TYPES.SET_PROVIDER_FILTER,
      payload: newProviders,
    });
  };

  const handleReferenceArchitectureChange = (
    checked: boolean,
    value: string,
  ) => {
    const newArchitectures = checked
      ? [...state.filters.referenceArchitectures, value]
      : state.filters.referenceArchitectures.filter((a) => a !== value);
    dispatch({
      type: ACTION_TYPES.SET_REFERENCE_ARCHITECTURE_FILTER,
      payload: newArchitectures,
    });
  };

  const filteredItems = state.items.filter((item) => {
    const matchesSearch =
      state.search === "" ||
      item.title.toLowerCase().includes(state.search.toLowerCase()) ||
      item.description.toLowerCase().includes(state.search.toLowerCase());

    const matchesProvider =
      state.filters.providers.length === 0 ||
      (item.provider && state.filters.providers.includes(item.provider));

    return matchesSearch && matchesProvider;
  });

  const providerCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    state.items.forEach((item) => {
      if (item.provider) {
        counts[item.provider] = (counts[item.provider] || 0) + 1;
      }
    });
    return counts;
  }, [state.items]);

  const referenceArchitectureCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    state.items.forEach((item) => {
      item.tags.forEach((tag) => {
        counts[tag] = (counts[tag] || 0) + 1;
      });
    });
    return counts;
  }, [state.items]);

  const totalSelectedFilters =
    state.filters.providers.length +
    state.filters.referenceArchitectures.length;

  return (
    <CatalogBrowseLayout
      title="Services"
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
          <AccordionItem title="Provider" open>
            <CheckboxGroup legendText="">
              <Checkbox
                labelText={`IBM (${providerCounts["IBM"] || 0})`}
                id="provider-ibm"
                checked={state.filters.providers.includes("IBM")}
                onChange={(_, { checked }) =>
                  handleProviderChange(checked, "IBM")
                }
              />
            </CheckboxGroup>
          </AccordionItem>

          <AccordionItem title="Architectures">
            <CheckboxGroup legendText="">
              <Checkbox
                labelText={`Data and content mgmt (${referenceArchitectureCounts["Data and content mgmt"] || 0})`}
                id="arch-data"
                checked={state.filters.referenceArchitectures.includes(
                  "Data and content mgmt",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(
                    checked,
                    "Data and content mgmt",
                  )
                }
              />
              <Checkbox
                labelText={`Deep process integration (${referenceArchitectureCounts["Deep process integration"] || 0})`}
                id="arch-deep"
                checked={state.filters.referenceArchitectures.includes(
                  "Deep process integration",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(
                    checked,
                    "Deep process integration",
                  )
                }
              />
              <Checkbox
                labelText={`Digital assistant (${referenceArchitectureCounts["Digital assistant"] || 0})`}
                id="arch-digital"
                checked={state.filters.referenceArchitectures.includes(
                  "Digital assistant",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(
                    checked,
                    "Digital assistant",
                  )
                }
              />
              <Checkbox
                labelText={`Fraud detection (${referenceArchitectureCounts["Fraud detection"] || 0})`}
                id="arch-fraud"
                checked={state.filters.referenceArchitectures.includes(
                  "Fraud detection",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(checked, "Fraud detection")
                }
              />
              <Checkbox
                labelText={`Image and video analysis (${referenceArchitectureCounts["Image and video analysis"] || 0})`}
                id="arch-image"
                checked={state.filters.referenceArchitectures.includes(
                  "Image and video analysis",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(
                    checked,
                    "Image and video analysis",
                  )
                }
              />
              <Checkbox
                labelText={`Recommender system (${referenceArchitectureCounts["Recommender system"] || 0})`}
                id="arch-recommender"
                checked={state.filters.referenceArchitectures.includes(
                  "Recommender system",
                )}
                onChange={(_, { checked }) =>
                  handleReferenceArchitectureChange(
                    checked,
                    "Recommender system",
                  )
                }
              />
            </CheckboxGroup>
          </AccordionItem>
        </>
      }
      results={filteredItems.map((item) => (
        <CatalogCard
          key={item.id}
          id={item.id}
          title={item.title}
          description={item.description}
          tags={item.tags}
          category={item.category}
          tagsHeading="Architectures"
          onDeploy={(id) => console.log("Deploy", id)}
          onLearnMore={(id) => console.log("Learn more", id)}
        />
      ))}
      emptyMessage="No services found matching your criteria."
    />
  );
};

export default ServicesPage;
