import type { StepProps } from "../types";
import { SharedStepOne } from "../../Shared/steps/SharedStepOne";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  showNameError = false,
  showWorkerError = false,
  onWorkerErrorReset,
  workers = [],
  isLoadingWorkers = false,
  refetchWorkers = () => {},
}) => {
  return (
    <SharedStepOne
      title={title}
      formData={formData}
      onChange={onChange}
      version={deployOptions.version}
      versionLabel="Service version"
      showNameError={showNameError}
      showWorkerError={showWorkerError}
      onWorkerErrorReset={onWorkerErrorReset}
      workers={workers}
      isLoadingWorkers={isLoadingWorkers}
      refetchWorkers={refetchWorkers}
    />
  );
};
