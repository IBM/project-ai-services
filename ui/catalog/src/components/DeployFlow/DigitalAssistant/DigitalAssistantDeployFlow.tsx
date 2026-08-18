import { useReducer, useEffect, useRef, useMemo } from "react";
import type { DeployFlowState, DeployFlowAction } from "./types.ts";
import type { BaseDeployFlowProps, DeployFormData } from "../Shared/types";
import type { ProviderSchema } from "@/types/api.types";
import { ACTION_TYPES } from "./types.ts";
import {
  sharedDeployFlowReducer,
  useDeployFlowReducer,
} from "../Shared/hooks/useDeployFlowReducer";
import { deployApplication, fetchServices } from "@/api/applications.api";
import { transformToDeploymentPayload } from "./utils/digitalAssistantDeploymentTransform";
import { runDeployment } from "../Shared/utils/runDeployment";
import { DeployTearsheetShell } from "../Shared/components/DeployTearsheetShell";
import { StepOne } from "./steps/StepOne";
import { StepTwo } from "./steps/StepTwo";
import { useDeployOptions } from "./hooks/useDeployOptions";
import { useDeployStore } from "@/store/deploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import { BASE_INITIAL_STATE } from "../Shared/utils/formData";
import { dedupe } from "@/utils/requestManager";

const STEPS = [
  {
    label: "Provide assistant details",
    description: "Configure basic settings",
  },
  {
    label: "Configure services",
    description: "Select and configure services",
  },
];
const STEP_ONE = 0;
const LAST_STEP = STEPS.length - 1;

const getInitialState = (formData: DeployFormData): DeployFlowState => ({
  ...BASE_INITIAL_STATE,
  isLoading: false,
  error: null,
  formData,
});

const daDeployFlowReducer = (
  state: DeployFlowState,
  action: DeployFlowAction,
): DeployFlowState => {
  switch (action.type) {
    case ACTION_TYPES.SET_IS_LOADING:
      return { ...state, isLoading: action.payload };
    case ACTION_TYPES.SET_ERROR:
      return { ...state, error: action.payload };
    case ACTION_TYPES.RESET_STATE:
      return getInitialState({
        name: "Digital assistant (copy)",
        version: "",
        globalComponents: {},
        services: {},
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
  const { deployOptions, isLoading, error } = useDeployOptions();

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
          console.error("Error fetching service summaries:", err);
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

  const initialState = useMemo(() => {
    if (deployOptions) {
      return getInitialState(initializeFormData(deployOptions));
    }
    return getInitialState({
      name: "Digital assistant (copy)",
      version: "",
      globalComponents: {},
      services: {},
    });
  }, [deployOptions]);

  const [state, dispatch] = useReducer(daDeployFlowReducer, initialState);
  const hasInitialized = useRef(false);

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

  useEffect(() => {
    dispatch({ type: ACTION_TYPES.SET_IS_LOADING, payload: isLoading });
  }, [isLoading]);

  useEffect(() => {
    dispatch({ type: ACTION_TYPES.SET_ERROR, payload: error ?? null });
  }, [error]);

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

    await runDeployment({
      dispatch,
      deploy: async () => {
        const providerParamsData: Record<string, ProviderSchema> = {};
        for (const [key, cache] of Object.entries(providerParams)) {
          providerParamsData[key] = cache.data;
        }
        const serviceParamsData: Record<string, Record<string, unknown>> = {};
        for (const [key, cache] of Object.entries(serviceParams)) {
          serviceParamsData[key] = cache.data;
        }
        const deploymentPayload = transformToDeploymentPayload(
          state.formData,
          deployOptions,
          providerParamsData,
          serviceParamsData,
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
    hasInitialized.current = false;
    onClose();
  };

  const isLastStep = state.currentStep === LAST_STEP;

  return (
    <DeployTearsheetShell
      open={open}
      onClose={handleClose}
      title="Deploy digital assistant"
      steps={STEPS}
      currentStep={state.currentStep}
      isLastStep={isLastStep}
      isDeploying={state.isDeploying}
      isPrimaryDisabled={state.isLoading || (isLastStep && state.isEditing)}
      onBack={handleBack}
      onNext={() => handleNext(state.formData.name)}
      onSubmit={handleSubmit}
      deployError={state.deployError}
      deployToastOpen={state.deployToastOpen}
      onRetryDeploy={handleSubmit}
      onDismissToast={() => dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST })}
      isLoading={state.isLoading}
      error={state.error}
    >
      {state.currentStep === STEP_ONE && deployOptions && (
        <StepOne
          title="Provide assistant details"
          formData={state.formData}
          onChange={handleFormDataChange}
          deployOptions={deployOptions}
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
        />
      )}
    </DeployTearsheetShell>
  );
};
