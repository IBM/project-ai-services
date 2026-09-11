import { useReducer, useEffect, useRef, useMemo, useState } from "react";
import type { DeployFlowAction } from "./types";
import type {
  BaseDeployFlowProps,
  BaseDeployFlowState,
  DeployFormData,
} from "../Shared/types";
import type { ProviderSchema } from "@/types/api.types";
import { ACTION_TYPES } from "./types";
import {
  sharedDeployFlowReducer,
  useDeployFlowReducer,
} from "../Shared/hooks/useDeployFlowReducer";
import { deployApplication, fetchServices } from "@/api/applications.api";
import { transformToDeploymentPayload } from "./utils/digitalAssistantDeploymentTransform";
import { runDeployment } from "../Shared/utils/runDeployment";
import { DeployTearsheetShell } from "../Shared/components/DeployTearsheetShell";
import { StepOne } from "./steps/DAStepOne";
import { DAStepTwo as StepTwo } from "./steps/DAStepTwo";
import { SharedDatasourceStep as StepThree } from "../Shared/steps/SharedDatasourceStep";
import { useDeployOptions } from "./hooks/useDeployOptions";
import { useDeployStore } from "@/store/deploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import {
  BASE_INITIAL_STATE,
  DEFAULT_FORM_DATA,
} from "../Shared/utils/formData";
import { dedupe } from "@/utils/requestManager";
import { useWorkers } from "@/hooks/useWorkers";

const BASE_STEPS = [
  {
    label: "Provide assistant details",
    description: "Configure basic settings",
  },
  {
    label: "Configure services",
    description: "Select and configure services",
  },
];

const DATASOURCE_STEP = {
  label: "Select data sources",
  description: "Connect data sources to your assistant",
};

const STEP_ONE = 0;
const STEP_TWO = 1;

const getInitialState = (formData: DeployFormData): BaseDeployFlowState => ({
  ...BASE_INITIAL_STATE,
  formData,
});

const daDeployFlowReducer = (
  state: BaseDeployFlowState,
  action: DeployFlowAction,
): BaseDeployFlowState => {
  switch (action.type) {
    case ACTION_TYPES.RESET_STATE:
      return getInitialState({
        name: "Digital assistant (copy)",
        version: "",
        globalComponents: {},
        services: {},
        ...DEFAULT_FORM_DATA,
        dataSources: [],
        uploadFromSourceEnabled: false,
      });
    default:
      return sharedDeployFlowReducer(state, action);
  }
};

export const DeployFlow = ({
  open,
  onClose,
  onSubmit,
}: BaseDeployFlowProps) => {
  const [hasStep1SchemaError, setHasStep1SchemaError] = useState(false);
  const [hasStep2SchemaError, setHasStep2SchemaError] = useState(false);
  const [hasStep3SchemaError, setHasStep3SchemaError] = useState(false);
  // Flipped to true on the first deploy attempt when toggle is on but nothing
  // is selected. Cleared when the user changes the toggle or closes the flow.
  const [showStep3SelectionError, setShowStep3SelectionError] = useState(false);

  const {
    workers,
    isLoading: isLoadingWorkers,
    refetch: refetchWorkers,
  } = useWorkers();

  const {
    serviceSummaries,
    setServiceSummaries,
    setServiceSummariesLoading,
    setServiceSummariesError,
    isServiceSummariesStale,
    providerParams,
    serviceParams,
    initialize,
  } = useDeployStore();

  // useReducer must come before useDeployOptions so runtime can be derived from state
  const initialState = useMemo(
    () =>
      getInitialState({
        name: "Digital assistant (copy)",
        version: "",
        globalComponents: {},
        services: {},
        ...DEFAULT_FORM_DATA,
      }),
    [],
  );
  const [state, dispatch] = useReducer(daDeployFlowReducer, initialState);
  const hasInitialized = useRef(false);

  const runtime = state.formData.deploymentType;

  const { deployOptions, isLoading, isProviderParamsLoading, error } =
    useDeployOptions(open, runtime);

  const hasDatasourceStep = useMemo(
    () =>
      deployOptions?.services.some((s) => s.accepts_datasource === true) ??
      false,
    [deployOptions],
  );

  const steps = useMemo(
    () => (hasDatasourceStep ? [...BASE_STEPS, DATASOURCE_STEP] : BASE_STEPS),
    [hasDatasourceStep],
  );

  const LAST_STEP = steps.length - 1;

  // Build once here and pass down — both StepOne and StepTwo need the same map.
  const providerParamsByType = useMemo(() => {
    if (!deployOptions) return {};
    const result: Record<string, Record<string, ProviderSchema>> = {};
    const allComponents = [
      ...deployOptions.global_components,
      ...deployOptions.services.flatMap((s) => s.components),
    ];
    allComponents.forEach((component) => {
      if (!result[component.type]) result[component.type] = {};
      component.providers.forEach((provider) => {
        const cached =
          providerParams[`${runtime}:${component.type}:${provider.id}`];
        if (cached) result[component.type][provider.id] = cached.data;
      });
    });
    return result;
  }, [deployOptions, providerParams, runtime]);

  // Initialize store and validate cache version on mount
  useEffect(() => {
    initialize();
  }, [initialize]);

  useEffect(() => {
    // Check if cache is stale
    const isStale = isServiceSummariesStale();

    // Fetch service summaries if not in store or stale
    // dedupe() handles preventing duplicate in-flight requests
    if (open && (serviceSummaries.length === 0 || isStale)) {
      setServiceSummariesLoading(true);

      dedupe("serviceSummaries", () => fetchServices())
        .then((data) => {
          setServiceSummaries(data);
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to load service descriptions";
          setServiceSummariesError(errorMessage);
        });
    }
  }, [
    open,
    serviceSummaries.length,
    setServiceSummaries,
    setServiceSummariesLoading,
    setServiceSummariesError,
    isServiceSummariesStale,
  ]);

  useEffect(() => {
    if (!open) {
      hasInitialized.current = false;
    }
  }, [open]);

  useEffect(() => {
    if (open && deployOptions && !hasInitialized.current) {
      hasInitialized.current = true;
      const formData = initializeFormData(deployOptions);
      dispatch({
        type: ACTION_TYPES.SET_FORM_DATA,
        payload: formData,
      });
    }
  }, [open, deployOptions]);

  const {
    handleNext,
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack,
  } = useDeployFlowReducer(dispatch, state.currentStep, STEP_ONE, LAST_STEP);

  const handleSubmit = async () => {
    if (!deployOptions) {
      dispatch({
        type: ACTION_TYPES.SET_DEPLOY_ERROR,
        payload: "Deploy options not loaded",
      });
      dispatch({ type: ACTION_TYPES.SHOW_DEPLOY_TOAST });
      return;
    }

    // If toggle is on but no data sources selected, show inline error and bail.
    const uploadEnabled = state.formData.uploadFromSourceEnabled ?? false;
    const hasDataSources = (state.formData.dataSources ?? []).length > 0;
    if (hasDatasourceStep && uploadEnabled && !hasDataSources) {
      setShowStep3SelectionError(true);
      return;
    }

    await runDeployment({
      dispatch,
      deploy: async () => {
        // Build service schemas for the active runtime only (keys are "runtime:serviceId")
        const runtimePrefix = `${runtime}:`;
        const serviceSchemas = Object.fromEntries(
          Object.entries(serviceParams)
            .filter(([key]) => key.startsWith(runtimePrefix))
            .map(([key, cache]) => [
              key.slice(runtimePrefix.length),
              cache.data,
            ]),
        );
        const deploymentPayload = transformToDeploymentPayload(
          state.formData,
          deployOptions,
          serviceSchemas,
          providerParamsByType,
        );
        await deployApplication(deploymentPayload);
      },
      onSuccess: () => {
        setShowStep3SelectionError(false);
        onSubmit();
        dispatch({ type: ACTION_TYPES.RESET_STATE });
        onClose();
      },
    });
  };

  const handleClose = () => {
    dispatch({ type: ACTION_TYPES.RESET_STATE });
    hasInitialized.current = false;
    setHasStep1SchemaError(false);
    setHasStep2SchemaError(false);
    setHasStep3SchemaError(false);
    setShowStep3SelectionError(false);
    onClose();
  };

  const isLastStep = state.currentStep === LAST_STEP;

  // Show the shell spinner while the top-level options load or while the
  // global-component provider schemas (needed by StepOne) are in-flight.
  // This mirrors ServicesDeployFlow's isStep1ComponentsLoading pattern so the
  // user sees a spinner rather than a populated step with a grey Next button.
  const shellIsLoading = isLoading || isProviderParamsLoading;

  const isPrimaryDisabled =
    shellIsLoading ||
    !!error ||
    (state.currentStep === STEP_ONE && hasStep1SchemaError) ||
    (state.currentStep === STEP_TWO &&
      (hasStep2SchemaError || state.isEditing)) ||
    (isLastStep && hasStep3SchemaError);

  return (
    <DeployTearsheetShell
      open={open}
      onClose={handleClose}
      title="Deploy digital assistant"
      steps={steps}
      currentStep={state.currentStep}
      isLastStep={isLastStep}
      isDeploying={state.isDeploying}
      isPrimaryDisabled={isPrimaryDisabled}
      onBack={handleBack}
      onNext={() => handleNext(state.formData.name, state.formData.workerName)}
      onSubmit={handleSubmit}
      deployError={state.deployError}
      deployToastOpen={state.deployToastOpen}
      onRetryDeploy={handleSubmit}
      onDismissToast={() => dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST })}
      isLoading={shellIsLoading}
      error={error}
    >
      {state.currentStep === STEP_ONE && deployOptions && (
        <StepOne
          title="Provide assistant details"
          formData={state.formData}
          onChange={handleFormDataChange}
          deployOptions={deployOptions}
          providerParamsByType={providerParamsByType}
          showNameError={state.showStepOneNameError}
          showWorkerError={state.showStepOneWorkerError}
          onWorkerErrorReset={() =>
            dispatch({
              type: ACTION_TYPES.SET_SHOW_STEP_ONE_WORKER_ERROR,
              payload: false,
            })
          }
          onComponentError={setHasStep1SchemaError}
          runtime={runtime}
          workers={workers}
          isLoadingWorkers={isLoadingWorkers}
          refetchWorkers={refetchWorkers}
        />
      )}
      {state.currentStep === STEP_TWO && deployOptions && (
        <StepTwo
          title="Configure services"
          formData={state.formData}
          onChange={handleFormDataChange}
          deployOptions={deployOptions}
          providerParamsByType={providerParamsByType}
          onEditingChange={handleEditingChange}
          onResourceStatusChange={handleResourceStatusChange}
          onComponentError={setHasStep2SchemaError}
          runtime={runtime}
        />
      )}
      {isLastStep && hasDatasourceStep && (
        <StepThree
          title="Select data sources"
          formData={state.formData}
          onChange={(updates) => {
            // Clear the selection error as soon as the user interacts
            // (toggles off, or picks a source).
            if (
              updates.uploadFromSourceEnabled === false ||
              (updates.dataSources && updates.dataSources.length > 0)
            ) {
              setShowStep3SelectionError(false);
            }
            handleFormDataChange(updates);
          }}
          onComponentError={setHasStep3SchemaError}
          showSelectionError={showStep3SelectionError}
        />
      )}
    </DeployTearsheetShell>
  );
};
