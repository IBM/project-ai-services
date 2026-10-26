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
  workers,
  isLoadingWorkers,
  refetchWorkers,
}) => {
  return (
    <SharedStepOne
      title={title}
      formData={formData}
      onChange={onChange}
      version={deployOptions.version}
      versionLabel="Digital assistant version"
      components={[]}
      onComponentChange={() => undefined}
      showNameError={showNameError}
      showWorkerError={showWorkerError}
      onWorkerErrorReset={onWorkerErrorReset}
      workers={workers}
      isLoadingWorkers={isLoadingWorkers}
      refetchWorkers={refetchWorkers}
    />
  );
};
