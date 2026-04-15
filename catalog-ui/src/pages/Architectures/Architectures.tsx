import { useReducer, useMemo } from "react";
import { AccordionItem, CheckboxGroup, Checkbox } from "@carbon/react";
import { CatalogCard, CatalogBrowseLayout } from "@/components";
import { ACTION_TYPES, INITIAL_STATE, pageReducer } from "./types";

const ArchitecturesPage = () => {
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

  const handleServiceChange = (checked: boolean, value: string) => {
    const newServices = checked
      ? [...state.filters.services, value]
      : state.filters.services.filter((s) => s !== value);
    dispatch({
      type: ACTION_TYPES.SET_SERVICE_FILTER,
      payload: newServices,
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

  const serviceCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    state.items.forEach((item) => {
      item.tags.forEach((tag) => {
        counts[tag] = (counts[tag] || 0) + 1;
      });
    });
    return counts;
  }, [state.items]);

  const totalSelectedFilters =
    state.filters.providers.length + state.filters.services.length;

  return (
    <CatalogBrowseLayout
      title="Architectures"
      subtitle="Production-ready AI solutions"
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
              <Checkbox
                labelText={`IBM certified (any provider) (${providerCounts["IBM certified"] || 0})`}
                id="provider-ibm-certified"
                checked={state.filters.providers.includes("IBM certified")}
                onChange={(_, { checked }) =>
                  handleProviderChange(checked, "IBM certified")
                }
              />
            </CheckboxGroup>
          </AccordionItem>

          <AccordionItem title="Services">
            <CheckboxGroup legendText="">
              <Checkbox
                labelText={`Digitize documents (${serviceCounts["Digitize documents"] || 0})`}
                id="service-digitize"
                checked={state.filters.services.includes("Digitize documents")}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Digitize documents")
                }
              />
              <Checkbox
                labelText={`Extract and tag info (${serviceCounts["Extract and tag info"] || 0})`}
                id="service-extract"
                checked={state.filters.services.includes(
                  "Extract and tag info",
                )}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Extract and tag info")
                }
              />
              <Checkbox
                labelText={`Find similar items (${serviceCounts["Find similar items"] || 0})`}
                id="service-similar"
                checked={state.filters.services.includes("Find similar items")}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Find similar items")
                }
              />
              <Checkbox
                labelText={`Knowledge management (${serviceCounts["Knowledge management"] || 0})`}
                id="service-knowledge"
                checked={state.filters.services.includes(
                  "Knowledge management",
                )}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Knowledge management")
                }
              />
              <Checkbox
                labelText={`Question and answer (${serviceCounts["Question and answer"] || 0})`}
                id="service-qa"
                checked={state.filters.services.includes("Question and answer")}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Question and answer")
                }
              />
              <Checkbox
                labelText={`Translation (${serviceCounts["Translation"] || 0})`}
                id="service-translation"
                checked={state.filters.services.includes("Translation")}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Translation")
                }
              />
              <Checkbox
                labelText={`Summarization (${serviceCounts["Summarization"] || 0})`}
                id="service-summarization"
                checked={state.filters.services.includes("Summarization")}
                onChange={(_, { checked }) =>
                  handleServiceChange(checked, "Summarization")
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
          isCertified={item.isCertified}
          tagsHeading="Services"
          onDeploy={(id) => console.log("Deploy", id)}
          onLearnMore={(id) => console.log("Learn more", id)}
        />
      ))}
      emptyMessage="No architectures found matching your criteria."
    />
  );
};

export default ArchitecturesPage;
