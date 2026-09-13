import {
  useReducer,
  useEffect,
  useRef,
  useMemo,
  useState,
  useCallback,
} from "react";
import { COMPONENT_TYPES } from "@/constants";
import { useWorkers } from "@/hooks/useWorkers";
import type {
  ServicesDeployFlowProps,
  DeployFlowState,
  DeployFlowAction,
} from "./types.ts";
import { ACTION_TYPES } from "./types.ts";
import {
  sharedDeployFlowReducer,
  useDeployFlowReducer,
} from "../Shared/hooks/useDeployFlowReducer";
import { deployApplication } from "@/api/applications.api";
import { transformToDeploymentPayload } from "./utils/serviceDeploymentTransform";
import { runDeployment } from "../Shared/utils/runDeployment";
import { DeployTearsheetShell } from "../Shared/components/DeployTearsheetShell";
import { StepOne } from "./steps/ServicesStepOne";
import { ServicesStepTwo as StepTwo } from "./steps/ServicesStepTwo";
import { StepZero } from "./steps/StepZero";
import { SharedDatasourceStep } from "../Shared/steps/SharedDatasourceStep";
import { useServiceDeployOptions } from "./hooks/useServiceDeployOptions";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import {
  BASE_INITIAL_STATE,
  DEFAULT_FORM_DATA,
} from "../Shared/utils/formData";

const BASE_STEPS = [
  {
    label: "Select service",
    description: "Choose a service to deploy",
  },
  {
    label: "Provide service details",
    description: "Configure basic settings",
  },
  {
    label: "Configure service",
    description: "Select and configure service",
  },
];

const DATASOURCE_STEP = {
  label: "Select data sources",
  description: "Connect data sources to your service",
};

const STEP_ONE = 1;

const getInitialState = (): DeployFlowState => ({
  ...BASE_INITIAL_STATE,
  formData: {
    name: "Service deployment (copy)",
    version: "",
    globalComponents: {},
    services: {},
    ...DEFAULT_FORM_DATA,
    dataSources: [],
    uploadFromSourceEnabled: false,
  },
  selectedServiceId: null,
  currentStep: 0,
  hasDatasourceStepError: false,
  showDatasourceSelectionError: false,
});

const servicesDeployFlowReducer = (
  state: DeployFlowState,
  action: DeployFlowAction,
): DeployFlowState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SELECTED_SERVICE:
      return { ...state, selectedServiceId: action.payload };
    case ACTION_TYPES.SET_DATASOURCE_STEP_ERROR:
      return { ...state, hasDatasourceStepError: action.payload };
    case ACTION_TYPES.SET_SHOW_DATASOURCE_SELECTION_ERROR:
      return { ...state, showDatasourceSelectionError: action.payload };
    case ACTION_TYPES.RESET_STATE:
      return getInitialState();
    default:
      return sharedDeployFlowReducer(state, action);
  }
};

export const ServicesDeployFlow = ({
  open,
  onClose,
  onSubmit,
  preSelectedServiceId,
}: ServicesDeployFlowProps) => {
  const [hasStep2SchemaError, setHasStep2SchemaError] = useState(false);

  const {
    workers,
    isLoading: isLoadingWorkers,
    refetch: refetchWorkers,
  } = useWorkers();

  const [state, dispatch] = useReducer(servicesDeployFlowReducer, {
    ...getInitialState(),
    selectedServiceId: preSelectedServiceId ?? null,
    currentStep: preSelectedServiceId ? STEP_ONE : 0,
  });

  // Track if form data has been initialized for the current service to prevent re-initialization
  const hasInitializedFormData = useRef<string | null>(null);

  const runtime = state.formData.deploymentType;

  // Only fetch deploy options when on step 1 or later (after user clicks Next)
  const shouldFetchDeployOptions =
    state.currentStep >= STEP_ONE && state.selectedServiceId;
  const { deployOptions, llmModels, isLoading, error, llmError } =
    useServiceDeployOptions(
      shouldFetchDeployOptions ? state.selectedServiceId : null,
      open,
      runtime,
    );

  // Derive the dynamic step list once deploy options are loaded.
  // Must be declared after deployOptions to avoid a TDZ ReferenceError.
  const hasDatasourceStep = useMemo(
    () => deployOptions?.accepts_datasource === true,
    [deployOptions],
  );

  const steps = useMemo(
    () => (hasDatasourceStep ? [...BASE_STEPS, DATASOURCE_STEP] : BASE_STEPS),
    [hasDatasourceStep],
  );

  const LAST_STEP = steps.length - 1;

  // Get component models loading and error state from store
  const componentModelsLoading = useServiceDeployStore(
    (state) => state.componentModelsLoading,
  );
  const componentModelsError = useServiceDeployStore(
    (state) => state.componentModelsError,
  );

  // Get services from store to access service description
  const services = useServiceDeployStore((state) => state.services);

  // Find the selected service to get its description
  const selectedService = services?.find(
    (service) => service.id === state.selectedServiceId,
  );

  // Check if any Step 1 components are still loading or have errored
  const step1Components = useMemo(() => {
    if (!deployOptions) return [];
    return (
      deployOptions.components?.filter(
        (c) =>
          c.type !== COMPONENT_TYPES.LLM && c.type !== COMPONENT_TYPES.RERANKER,
      ) || []
    );
  }, [deployOptions]);

  const isStep1ComponentsLoading = useMemo(() => {
    if (!state.selectedServiceId || !step1Components.length) return false;
    return step1Components.some((component) => {
      const key = `${state.selectedServiceId}:${component.type}:${runtime}`;
      return componentModelsLoading[key] === true;
    });
  }, [
    state.selectedServiceId,
    step1Components,
    componentModelsLoading,
    runtime,
  ]);

  const hasStep1ComponentsError = useMemo(() => {
    if (!state.selectedServiceId || !step1Components.length) return false;
    return step1Components.some((component) => {
      const key = `${state.selectedServiceId}:${component.type}:${runtime}`;
      return !!componentModelsError[key];
    });
  }, [state.selectedServiceId, step1Components, componentModelsError, runtime]);

  useEffect(() => {
    if (open && preSelectedServiceId) {
      dispatch({
        type: ACTION_TYPES.SET_SELECTED_SERVICE,
        payload: preSelectedServiceId,
      });
      dispatch({ type: ACTION_TYPES.SET_CURRENT_STEP, payload: STEP_ONE });
    } else if (!open) {
      hasInitializedFormData.current = null;
      dispatch({ type: ACTION_TYPES.RESET_STATE });
    }
  }, [open, preSelectedServiceId]);

  // Initialize form data dynamically when deploy options are loaded (only once per service)
  useEffect(() => {
    if (
      open &&
      state.currentStep >= STEP_ONE &&
      deployOptions &&
      state.selectedServiceId &&
      hasInitializedFormData.current !== state.selectedServiceId
    ) {
      hasInitializedFormData.current = state.selectedServiceId;

      // Initialize form data dynamically from API response
      const formData = initializeFormData(
        deployOptions,
        state.selectedServiceId,
      );

      dispatch({
        type: ACTION_TYPES.SET_FORM_DATA,
        payload: formData,
      });
    }
  }, [open, state.currentStep, deployOptions, state.selectedServiceId]);

  const providerSchemas = useServiceDeployStore(
    (state) => state.providerSchemas,
  );

  // Helper function to check if all required credential fields are filled for all services
  const areAllRequiredFieldsFilled = useMemo(() => {
    if (
      !state.selectedServiceId ||
      !state.formData.services[state.selectedServiceId]
    ) {
      return true; // If no service selected, allow proceeding
    }

    const serviceConfig = state.formData.services[state.selectedServiceId];
    const llmComponent = serviceConfig?.components?.llm;

    if (!llmComponent?.providerId) {
      return true; // If no LLM provider selected, allow proceeding
    }

    // Get the provider schema for the selected LLM provider
    const schemaKey = `${state.selectedServiceId}:llm:${llmComponent.providerId}:${runtime}`;
    const providerSchema = providerSchemas[schemaKey];

    if (!providerSchema || !providerSchema.required) {
      return true; // If no schema or no required fields, allow proceeding
    }

    const requiredFields = providerSchema.required;
    // Credentials land in serviceConfig.params; model lands in llmComponent.params.
    // Check both so required fields are found regardless of which bag they're in.
    const allParams = {
      ...(llmComponent.params || {}),
      ...(serviceConfig.params || {}),
    };

    return requiredFields.every((fieldKey) => {
      const value = allParams[fieldKey];
      return (
        value !== undefined && value !== null && String(value).trim() !== ""
      );
    });
  }, [
    state.selectedServiceId,
    state.formData.services,
    providerSchemas,
    runtime,
  ]);

  const {
    handleNext,
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack,
  } = useDeployFlowReducer(dispatch, state.currentStep, STEP_ONE, LAST_STEP);

  const handleServiceSelect = (serviceId: string) => {
    dispatch({ type: ACTION_TYPES.SET_SELECTED_SERVICE, payload: serviceId });
  };

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
      dispatch({
        type: ACTION_TYPES.SET_SHOW_DATASOURCE_SELECTION_ERROR,
        payload: true,
      });
      return;
    }

    await runDeployment({
      dispatch,
      deploy: async () => {
        const runtimeSuffix = `:${runtime}`;
        const resolvedSchemas = Object.fromEntries(
          Object.entries(providerSchemas)
            .filter(([key]) => key.endsWith(runtimeSuffix))
            .map(([key, schema]) => [
              key.slice(0, key.length - runtimeSuffix.length),
              schema,
            ]),
        );
        const deploymentPayload = await transformToDeploymentPayload(
          state.formData,
          deployOptions,
          resolvedSchemas,
          state.selectedServiceId,
        );
        await deployApplication(deploymentPayload);
      },
      onSuccess: () => {
        onSubmit();
        dispatch({ type: ACTION_TYPES.RESET_STATE });
        onClose();
      },
    });
  };

  const handleClose = () => {
    dispatch({ type: ACTION_TYPES.RESET_STATE });
    hasInitializedFormData.current = null;
    setHasStep2SchemaError(false);
    onClose();
  };

  const isLastStep = state.currentStep === LAST_STEP;

  const onLoadingStep = state.currentStep > 0;
  const shellIsLoading =
    onLoadingStep &&
    ((!deployOptions && isLoading) || isStep1ComponentsLoading);
  const shellError = onLoadingStep ? error : null;
  const hasLlmError = onLoadingStep ? !!llmError : false;

  const isPrimaryDisabled =
    (state.currentStep === 0 && !state.selectedServiceId) ||
    !!shellError ||
    (state.currentStep === STEP_ONE && hasStep1ComponentsError) ||
    (isLastStep && !hasDatasourceStep && state.isEditing) ||
    (isLastStep && !hasDatasourceStep && !areAllRequiredFieldsFilled) ||
    (isLastStep &&
      !hasDatasourceStep &&
      (hasStep2SchemaError || hasLlmError)) ||
    (isLastStep && hasDatasourceStep && state.hasDatasourceStepError);

  const stepsWithCompletion = [
    { ...steps[0], complete: !!state.selectedServiceId },
    ...steps.slice(1),
  ];

  // The configure step is always the step just before the optional datasource step.
  const CONFIGURE_STEP = hasDatasourceStep ? LAST_STEP - 1 : LAST_STEP;

  const handleWorkerErrorReset = useCallback(
    () =>
      dispatch({
        type: ACTION_TYPES.SET_SHOW_STEP_ONE_WORKER_ERROR,
        payload: false,
      }),
    [dispatch],
  );

  return (
    <DeployTearsheetShell
      open={open}
      onClose={handleClose}
      title="Deploy service"
      steps={stepsWithCompletion}
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
      error={shellError}
    >
      {state.currentStep === 0 && (
        <StepZero
          title="Select service"
          selectedServiceId={state.selectedServiceId}
          onServiceSelect={handleServiceSelect}
          isOpen={open}
        />
      )}
      {state.currentStep === STEP_ONE && deployOptions && (
        <StepOne
          title="Provide service details"
          formData={state.formData}
          onChange={handleFormDataChange}
          deployOptions={deployOptions}
          selectedServiceId={state.selectedServiceId}
          showNameError={state.showStepOneNameError}
          showWorkerError={state.showStepOneWorkerError}
          onWorkerErrorReset={handleWorkerErrorReset}
          runtime={runtime}
          workers={workers}
          isLoadingWorkers={isLoadingWorkers}
          refetchWorkers={refetchWorkers}
        />
      )}
      {state.currentStep === CONFIGURE_STEP && deployOptions && (
        <StepTwo
          title="Configure services"
          formData={state.formData}
          onChange={handleFormDataChange}
          deployOptions={deployOptions}
          onEditingChange={handleEditingChange}
          onResourceStatusChange={handleResourceStatusChange}
          selectedServiceId={state.selectedServiceId}
          llmModelsWithProviders={llmModels}
          serviceDescription={selectedService?.description}
          isLoadingLlmModels={!!isLoading}
          onComponentError={setHasStep2SchemaError}
          runtime={runtime}
        />
      )}
      {isLastStep && hasDatasourceStep && (
        <SharedDatasourceStep
          title="Select data sources"
          formData={state.formData}
          onChange={(updates) => {
            // Clear the selection error as soon as the user interacts
            // (toggles off, or picks a source).
            if (
              updates.uploadFromSourceEnabled === false ||
              (updates.dataSources && updates.dataSources.length > 0)
            ) {
              dispatch({
                type: ACTION_TYPES.SET_SHOW_DATASOURCE_SELECTION_ERROR,
                payload: false,
              });
            }
            handleFormDataChange(updates);
          }}
          onComponentError={(hasError) =>
            dispatch({
              type: ACTION_TYPES.SET_DATASOURCE_STEP_ERROR,
              payload: hasError,
            })
          }
          showSelectionError={state.showDatasourceSelectionError}
        />
      )}
    </DeployTearsheetShell>
  );
};
