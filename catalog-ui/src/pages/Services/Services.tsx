import { useReducer } from "react";
import { useNavigate } from "react-router-dom";
import { PageHeader } from "@carbon/ibm-products";
import {
  Grid,
  Column,
  Search,
  Accordion,
  AccordionItem,
  CheckboxGroup,
  Checkbox,
  Button,
} from "@carbon/react";
import { ArrowRight } from "@carbon/icons-react";
import { ServiceCard } from "@/components";
import styles from "./Services.module.scss";
import { ACTION_TYPES, INITIAL_STATE, pageReducer } from "./types";

const providerOptions = [
  { id: "provider-ibm", label: "IBM", count: 4, value: "IBM" },
  { id: "provider-private", label: "Private", count: 0, value: "Private" },
  {
    id: "provider-public",
    label: "Public (third-party)",
    count: 0,
    value: "Public (third-party)",
  },
  {
    id: "provider-certified",
    label: "IBM certified (any provider)",
    count: 4,
    value: "IBM certified (any provider)",
  },
];

const architectureOptions = [
  { id: "arch-data", label: "Data and content mgmt", count: 0 },
  { id: "arch-deep", label: "Deep process integration", count: 0 },
  { id: "arch-digital", label: "Digital assistant", count: 4 },
  { id: "arch-forecasting", label: "Forecasting", count: 0 },
  { id: "arch-fraud", label: "Fraud detection", count: 2 },
  { id: "arch-image", label: "Image and video analysis", count: 0 },
  { id: "arch-recommender", label: "Recommender system", count: 0 },
];

const ServicesPage = () => {
  const [state, dispatch] = useReducer(pageReducer, INITIAL_STATE);
  const navigate = useNavigate();

  const handleDeploy = (id: string) => {
    const service = state.items.find((item) => item.id === id);
    if (service) {
      navigate("/ai-deployments", {
        state: {
          deploy: {
            id: service.id,
            title: service.title,
            description: service.description,
          },
        },
      });
    }
  };

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

  return (
    <>
      <PageHeader
        title={{ text: "Services" }}
        subtitle="Pre-built AI demos from real-world use cases to help you envision how AI can solve common business problems."
        pageActions={[
          {
            key: "learn-more",
            kind: "tertiary",
            label: "Learn more",
            renderIcon: ArrowRight,
            onClick: () => {
              window.open(
                "https://www.ibm.com/docs/en/aiservices?topic=services-introduction",
                "_blank",
              );
            },
          },
        ]}
        pageActionsOverflowLabel="More actions"
        fullWidthGrid="xl"
      />

      <div className={styles.pageContent}>
        <Grid fullWidth>
          <Column lg={4} md={2} sm={4} className={styles.sidebarColumn}>
            <aside className={styles.sidebar}>
              <Search
                className={styles.sidebarSearch}
                placeholder="Search"
                labelText="Search"
                value={state.search}
                onChange={(e) =>
                  dispatch({
                    type: ACTION_TYPES.SET_SEARCH,
                    payload: e.target.value,
                  })
                }
                size="lg"
              />

              <div className={styles.filtersLabel}>
                <span className={styles.filtersLabelText}>Filters</span>
              </div>

              <Accordion className={styles.filtersAccordion}>
                <AccordionItem title="Provider" open>
                  <CheckboxGroup legendText="">
                    {providerOptions.map((option) => {
                      const isDisabled = option.count === 0;
                      const label = `${option.label}${option.count > 0 ? ` (${option.count})` : ""}`;

                      return (
                        <Checkbox
                          key={option.id}
                          labelText={label}
                          id={option.id}
                          disabled={isDisabled}
                          checked={state.filters.providers.includes(
                            option.value,
                          )}
                          onChange={(_, { checked }) =>
                            handleProviderChange(checked, option.value)
                          }
                        />
                      );
                    })}
                  </CheckboxGroup>
                </AccordionItem>

                <AccordionItem title="Architectures" open>
                  <CheckboxGroup legendText="">
                    {architectureOptions.map((option) => {
                      const label = `${option.label}${option.count > 0 ? ` (${option.count})` : ""}`;

                      return (
                        <Checkbox
                          key={option.id}
                          labelText={label}
                          id={option.id}
                          checked={state.filters.referenceArchitectures.includes(
                            option.label,
                          )}
                          onChange={(_, { checked }) =>
                            handleReferenceArchitectureChange(
                              checked,
                              option.label,
                            )
                          }
                        />
                      );
                    })}
                  </CheckboxGroup>
                </AccordionItem>
              </Accordion>
            </aside>
          </Column>

          <Column lg={12} md={6} sm={4} className={styles.contentColumn}>
            <div className={styles.cardsGrid}>
              {filteredItems.map((item) => (
                <ServiceCard
                  key={item.id}
                  id={item.id}
                  title={item.title}
                  description={item.description}
                  tags={item.tags}
                  category={item.category}
                  isCertified={item.isCertified}
                  onDeploy={handleDeploy}
                  onLearnMore={(id: string) => console.log("Learn more", id)}
                />
              ))}
            </div>

            {filteredItems.length === 0 && (
              <div className={styles.emptyState}>
                <p>No services found matching your criteria.</p>
                <Button
                  kind="tertiary"
                  onClick={() => dispatch({ type: ACTION_TYPES.CLEAR_FILTERS })}
                >
                  Clear filters
                </Button>
              </div>
            )}
          </Column>
        </Grid>
      </div>

      {/* Deploy modal now handled by AI-deployments page */}
    </>
  );
};

export default ServicesPage;
