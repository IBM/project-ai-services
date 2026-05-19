import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import ServiceCard from "@/components/ServiceCard";
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
                  onDeploy={(id) => console.log("Deploy:", id)}
                  onLearnMore={(id) => console.log("Learn more:", id)}
                />
              ))}
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </div>
  );
};

export default Services;
