import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import styles from "./Models.module.scss";

const Models = () => {
  return (
    <div className={styles.modelsContainer}>
      <PageHeader
        title="Models"
        subtitle="Manage foundation AI models and deployed inference instances across IBM Power worker nodes."
        className={styles.pageHeader}
      />
      <Tabs>
        <TabList
          aria-label="Models tabs"
          contained={false}
          className={styles.tabList}
        >
          <Tab>Deployments</Tab>
          <Tab>Catalog</Tab>
        </TabList>
        <TabPanels>
          <TabPanel />
          <TabPanel />
        </TabPanels>
      </Tabs>
    </div>
  );
};

export default Models;
