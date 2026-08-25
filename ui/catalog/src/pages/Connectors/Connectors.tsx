import { PageHeader } from "@carbon/ibm-products";
import DataSourceConnectorsTable from "@/components/DataSourceConnectorsTable";
import styles from "./Connectors.module.scss";

const Connectors = () => {
  return (
    <div className={styles.connectorsContainer}>
      <PageHeader title="Connectors" />
      <DataSourceConnectorsTable />
    </div>
  );
};

export default Connectors;
