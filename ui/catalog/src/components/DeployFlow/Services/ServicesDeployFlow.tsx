import { useReducer, useEffect, useRef, useMemo, useState } from "react";
import { COMPONENT_TYPES } from "@/constants";
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
import { useServiceDeployOptions } from "./hooks/useServiceDeployOptions";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import { BASE_INITIAL_STATE } from "../Shared/utils/formData";

const STEPS = [
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
const STEP_ONE = 1;
const LAST_STEP = STEPS.length - 1;

const getInitialState = (): DeployFlowState => ({
  ...BASE_INITIAL_STATE,
  formData: {
    name: "Service deployment (copy)",
    version: "",
    globalComponents: {},
    services: {},
  },
  selectedServiceId: null,
  currentStep: 0,
});

const servicesDeployFlowReducer = (
  state: DeployFlowState,
  action: DeployFlowAction,
): DeployFlowState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SELECTED_SERVICE:
      return { ...state, selectedServiceId: action.payload };
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
  const [state, dispatch] = useReducer(servicesDeployFlowReducer, {
    ...getInitialState(),
    selectedServiceId: preSelectedServiceId ?? null,
    currentStep: preSelectedServiceId ? STEP_ONE : 0,
  });

  // Track if form data has been initialized for the current service to prevent re-initialization
  const hasInitializedFormData = useRef<string | null>(null);

  // Only fetch deploy options when on step 1 or later (after user clicks Next)
  const shouldFetchDeployOptions =
    state.currentStep >= STEP_ONE && state.selectedServiceId;
  const { deployOptions, llmModels, isLoading, error, llmError } =
    useServiceDeployOptions(
      shouldFetchDeployOptions ? state.selectedServiceId : null,
      open,
    );

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
      const key = `${state.selectedServiceId}:${component.type}`;
      return componentModelsLoading[key] === true;
    });
  }, [state.selectedServiceId, step1Components, componentModelsLoading]);

  const hasStep1ComponentsError = useMemo(() => {
    if (!state.selectedServiceId || !step1Components.length) return false;
    return step1Components.some((component) => {
      const key = `${state.selectedServiceId}:${component.type}`;
      return !!componentModelsError[key];
    });
  }, [state.selectedServiceId, step1Components, componentModelsError]);

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
    const schemaKey = `${state.selectedServiceId}:llm:${llmComponent.providerId}`;
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
  }, [state.selectedServiceId, state.formData.services, providerSchemas]);

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

    await runDeployment({
      dispatch,
      deploy: async () => {
        const deploymentPayload = await transformToDeploymentPayload(
          state.formData,
          deployOptions,
          providerSchemas,
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
    (isLastStep && state.isEditing) ||
    (isLastStep && !areAllRequiredFieldsFilled) ||
    (isLastStep && (hasStep2SchemaError || hasLlmError));

  const steps = [
    { ...STEPS[0], complete: !!state.selectedServiceId },
    ...STEPS.slice(1),
  ];

  return (
    <DeployTearsheetShell
      open={open}
      onClose={handleClose}
      title="Deploy service"
      steps={steps}
      currentStep={state.currentStep}
      isLastStep={isLastStep}
      isDeploying={state.isDeploying}
      isPrimaryDisabled={isPrimaryDisabled}
      onBack={handleBack}
      onNext={() => handleNext(state.formData.name)}
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
        />
      )}
      {state.currentStep === LAST_STEP && deployOptions && (
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
        />
      )}
    </DeployTearsheetShell>
  );
};
