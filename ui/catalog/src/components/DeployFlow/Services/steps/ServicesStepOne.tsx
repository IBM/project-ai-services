import { useMemo } from "react";
import type { StepProps } from "../types";
import {
  SharedStepOne,
  type StepOneComponentRow,
} from "../../Shared/steps/SharedStepOne";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  showNameError = false,
  showWorkerError = false,
  onWorkerErrorReset,
  onComponentError,
  workers = [],
  isLoadingWorkers = false,
  refetchWorkers = () => {},
}) => {
  // Step 1 no longer shows embedding or vector store — those are editable in Step 2.
  // Build an empty components array so SharedStepOne renders no component dropdowns.
  const components = useMemo<StepOneComponentRow[]>(() => [], []);

  return (
    <SharedStepOne
      title={title}
      formData={formData}
      onChange={onChange}
      version={deployOptions.version}
      versionLabel="Service version"
      components={components}
      onComponentChange={() => {}}
      showNameError={showNameError}
      showWorkerError={showWorkerError}
      onWorkerErrorReset={onWorkerErrorReset}
      failedComponentNames={[]}
      onComponentError={onComponentError}
      workers={workers}
      isLoadingWorkers={isLoadingWorkers}
      refetchWorkers={refetchWorkers}
    />
  );
};
