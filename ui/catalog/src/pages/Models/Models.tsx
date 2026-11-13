import { useState } from "react";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader, NoDataEmptyState } from "@carbon/ibm-products";
import { ModelCard, ModelCardSkeleton } from "@/components/ModelCard";
import { useModels } from "@/hooks/useModels";
import { ModelsTable } from "@/components";
import styles from "./Models.module.scss";

const Models = () => {
  const [selectedTabIndex, setSelectedTabIndex] = useState(0);
  const { models, isLoading, error, refetch } = useModels();

  const handleTabChange = (evt: { selectedIndex: number }) => {
    setSelectedTabIndex(evt.selectedIndex);
    // Catalog tab is index 1 — fetch fresh data every time the user switches to it
    if (evt.selectedIndex === 1) {
      refetch();
    }
  };

  return (
    <div className={styles.modelsContainer}>
      <PageHeader
        title="Models"
        subtitle="Manage foundation AI models and deployed inference instances across IBM Power worker nodes."
        className={styles.pageHeader}
      />
      <Tabs selectedIndex={selectedTabIndex} onChange={handleTabChange}>
        <TabList
          aria-label="Models tabs"
          contained={false}
          className={styles.tabList}
        >
          <Tab>Model deployments</Tab>
          <Tab>Catalog</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <ModelsTable />
          </TabPanel>
          <TabPanel>
            {isLoading ? (
              <div className={styles.catalogGrid}>
                {Array.from({ length: 4 }).map((_, index) => (
                  <ModelCardSkeleton key={`skeleton-${index}`} />
                ))}
              </div>
            ) : error ? (
              <div className={styles.errorMessage}>{error}</div>
            ) : models.length === 0 ? (
              <NoDataEmptyState
                title="No models available"
                subtitle="There are no models in the catalog yet."
              />
            ) : (
              <div className={styles.catalogGrid}>
                {models.map((model) => (
                  <ModelCard
                    key={model.id}
                    id={model.id}
                    name={model.name}
                    componentType={model.component_type}
                    description={model.description}
                    certifiedBy={model.certified_by}
                    providers={model.providers}
                    onDeploy={() => {}}
                  />
                ))}
              </div>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>
    </div>
  );
};

export default Models;
