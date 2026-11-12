import { Tabs, TabList, Tab, TabPanels, TabPanel } from "@carbon/react";
import { PageHeader } from "@carbon/ibm-products";
import { PAGE_TITLES, PAGE_SUBTITLES } from "@/constants";
import styles from "./Models.module.scss";

const Models = () => {
  return (
    <div className={styles.modelsContainer}>
      <PageHeader
        title={PAGE_TITLES.MODELS}
        subtitle={PAGE_SUBTITLES.MODELS}
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
