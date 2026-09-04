import { PageHeader } from "@carbon/ibm-products";
import WorkerResourcesTable from "@/components/WorkerResourcesTable";

const WorkerResources = () => {
  return (
    <>
      <PageHeader title="Worker Resources" />
      <WorkerResourcesTable />
    </>
  );
};

export default WorkerResources;
