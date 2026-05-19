import { useState } from "react";
import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import ServiceCard from "@/components/ServiceCard";
import ServiceDetailPanel from "@/components/ServiceDetailPanel";
import styles from "./Services.module.scss";

const mockServices = [
  {
    id: "1",
    title: "Digitize documents",
    description:
      "Converts physical and scanned documents into searchable, editable  and digital formats;  enables efficient document management, data extraction, and workflow automation at scale.",
    isCertified: true,
  },
  {
    id: "2",
    title: "Find similar items",
    description:
      "Identifies and retrieves items similar to a reference item based on content, attributes, or patterns; enables efficient recommendation, duplicate detection, and content discovery at scale.",
    isCertified: true,
  },
  {
    id: "3",
    title: "Question and answer",
    description:
      "Provides accurate, context-aware responses to user queries; enables automated customer support, knowledge base interactions, and conversational assistance at scale.",
    isCertified: true,
  },
  {
    id: "4",
    title: "Summarize",
    description:
      "Condenses long-form content into concise, accurate summaries while preserving key information; enables efficient document analysis and information extraction at scale.",
    isCertified: true,
  },
];

const Services = () => {
  const [selectedService, setSelectedService] = useState<typeof mockServices[0] | null>(null);
  const [isPanelOpen, setIsPanelOpen] = useState(false);

  const handleCardClick = (id: string) => {
    const service = mockServices.find((s) => s.id === id);
    if (service) {
      setSelectedService(service);
      setIsPanelOpen(true);
    }
  };

  const handleDeploy = (id: string) => {
    console.log("Deploy service:", id);
    // Add your deploy logic here
  };

  const handleClosePanel = () => {
    setIsPanelOpen(false);
    // Optional: Clear selected service after animation completes
    setTimeout(() => setSelectedService(null), 300);
  };

  return (
    <div className={styles.servicesContainer}>
      <PageHeader
        title="Services"
        subtitle="Single-purpose AI capabilities designed to perform specific tasks independently or as part of larger solutions."
        className={styles.pageHeader}
      />
      <Tabs>
        <TabList
          aria-label="Services tabs"
          contained={false}
          className={styles.tabList}
        >
          <Tab>Deployments</Tab>
          <Tab>Catalog</Tab>
        </TabList>
        <TabPanels>
          <TabPanel>
            <div className={styles.tabContent}>Deployments content</div>
          </TabPanel>
          <TabPanel>
            <div className={styles.catalogGrid}>
              {mockServices.map((service) => (
                <ServiceCard
                  key={service.id}
                  id={service.id}
                  title={service.title}
                  description={service.description}
                  isCertified={service.isCertified}
                  onDeploy={handleDeploy}
                  onLearnMore={handleCardClick}
                />
              ))}
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>

      <ServiceDetailPanel
        open={isPanelOpen}
        onClose={handleClosePanel}
        service={selectedService}
        onDeploy={handleDeploy}
      />
    </div>
  );
};

export default Services;

// Made with Bob
