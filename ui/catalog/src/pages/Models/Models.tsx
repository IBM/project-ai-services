import { useState } from "react";
import type { ModelDeploymentRow } from "@/components/ModelsTable/types";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader, NoDataEmptyState } from "@carbon/ibm-products";
import { PAGE_TITLES, PAGE_SUBTITLES } from "@/constants";
import { ModelCard, ModelCardSkeleton } from "@/components/ModelCard";
import { ModelsTable, ModelDetailPanel, ModelDetails } from "@/components";
import { useModels } from "@/hooks/useModels";
import styles from "./Models.module.scss";

const Models = () => {
  const [selectedTabIndex, setSelectedTabIndex] = useState(0);
  const [selectedModelId, setSelectedModelId] = useState<string | null>(null);
  const [isPanelOpen, setIsPanelOpen] = useState(false);
  const [selectedModel, setSelectedModel] = useState<ModelDeploymentRow | null>(
    null,
  );
  const { models, isLoading, error, refetch } = useModels();

  const handleCardClick = (id: string) => {
    setSelectedModelId(id);
    setIsPanelOpen(true);
  };

  const handleClosePanel = () => {
    setIsPanelOpen(false);
  };

  const handleTabChange = (evt: { selectedIndex: number }) => {
    setSelectedTabIndex(evt.selectedIndex);
    // Catalog tab is index 1 — fetch fresh data every time the user switches to it
    if (evt.selectedIndex === 1) {
      refetch();
    }
  };

  if (selectedModel) {
    return (
      <ModelDetails
        key={selectedModel.id}
        modelId={selectedModel.id}
        modelName={selectedModel.name}
        onBack={() => setSelectedModel(null)}
      />
    );
  }

  return (
    <div className={styles.modelsContainer}>
      <PageHeader
        title={PAGE_TITLES.MODELS}
        subtitle={PAGE_SUBTITLES.MODELS}
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
            <ModelsTable onRowClick={setSelectedModel} />
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
                    onLearnMore={handleCardClick}
                  />
                ))}
              </div>
            )}
          </TabPanel>
        </TabPanels>
      </Tabs>

      <ModelDetailPanel
        open={isPanelOpen}
        onClose={handleClosePanel}
        modelId={selectedModelId}
      />
    </div>
  );
};

export default Models;
