import { useState } from "react";
import { PageHeader } from "@carbon/ibm-products";
import DataSourceConnectorsTable from "@/components/DataSourceConnectorsTable";
import AddDataSourceModal from "@/components/AddDataSourceModal";
import styles from "./Connectors.module.scss";

const Connectors = () => {
  const [isAddModalOpen, setIsAddModalOpen] = useState(false);
  const [refreshTrigger, setRefreshTrigger] = useState(0);

  return (
    <div className={styles.connectorsContainer}>
      <PageHeader title="Connectors" />
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
