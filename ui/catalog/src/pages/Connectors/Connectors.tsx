import { useState, useEffect } from "react";
import { PageHeader } from "@carbon/ibm-products";
import DataSourceConnectorsTable from "@/components/DataSourceConnectorsTable";
import AddDataSourceModal from "@/components/AddDataSourceModal";
import { useConnectorsStore } from "@/store/connectors.store";
import { PAGE_TITLES, PAGE_SUBTITLES } from "@/constants";
import styles from "./Connectors.module.scss";

const Connectors = () => {
  const [isAddModalOpen, setIsAddModalOpen] = useState(false);
  const [refreshTrigger, setRefreshTrigger] = useState(0);

  const { initialize } = useConnectorsStore();

  useEffect(() => {
    initialize();
  }, [initialize]);

  return (
    <div id="connectors-page-content" className={styles.connectorsContainer}>
      <PageHeader
        title={PAGE_TITLES.DATA_CONNECTORS}
        subtitle={PAGE_SUBTITLES.DATA_CONNECTORS}
      />
      <DataSourceConnectorsTable
        onAdd={() => setIsAddModalOpen(true)}
        refreshTrigger={refreshTrigger}
      />
      <AddDataSourceModal
        open={isAddModalOpen}
        onClose={() => setIsAddModalOpen(false)}
        onSuccess={() => setRefreshTrigger((n) => n + 1)}
      />
    </div>
  );
};

export default Connectors;
