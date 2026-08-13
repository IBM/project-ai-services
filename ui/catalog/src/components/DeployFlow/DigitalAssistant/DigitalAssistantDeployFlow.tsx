import { useReducer, useEffect, useRef, useMemo } from "react";
import { Tearsheet } from "@carbon/ibm-products";
import {
  ProgressIndicator,
  ProgressStep,
  InlineLoading,
  ActionableNotification,
} from "@carbon/react";
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
import { extractDeployError } from "../Shared/utils/deployError";
import { StepOne } from "./steps/StepOne";
import { StepTwo } from "./steps/StepTwo";
import { useDeployOptions } from "./hooks/useDeployOptions";
import { useDeployStore } from "@/store/deploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import { BASE_INITIAL_STATE } from "../Shared/utils/formData";
import { dedupe } from "@/utils/requestManager";
import styles from "./DigitalAssistantDeployFlow.module.scss";

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
    if (error) {
      dispatch({ type: ACTION_TYPES.SET_ERROR, payload: error });
    }
  }, [error]);

  const {
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack,
  } = useDeployFlowReducer(dispatch, state.currentStep);

  const handleNext = () => {
    if (!state.formData.name.trim()) {
      dispatch({
        type: ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR,
        payload: true,
      });
      return;
    }

    if (state.currentStep < 1) {
      dispatch({
        type: ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR,
        payload: false,
      });
      dispatch({
        type: ACTION_TYPES.SET_CURRENT_STEP,
        payload: state.currentStep + 1,
      });
    }
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

    dispatch({ type: ACTION_TYPES.SET_IS_DEPLOYING, payload: true });
    dispatch({ type: ACTION_TYPES.SET_DEPLOY_ERROR, payload: null });
    dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST });

    try {
      // Transform cached params to plain data objects (strip fetchedAt timestamps)
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

      onSubmit();
      dispatch({ type: ACTION_TYPES.RESET_STATE });
      onClose();
    } catch (error: unknown) {
      dispatch({
        type: ACTION_TYPES.SET_DEPLOY_ERROR,
        payload: extractDeployError(error),
      });
      dispatch({ type: ACTION_TYPES.SHOW_DEPLOY_TOAST });
      console.error("Deployment error:", error);
    } finally {
      dispatch({ type: ACTION_TYPES.SET_IS_DEPLOYING, payload: false });
    }
  };

  const handleClose = () => {
    dispatch({ type: ACTION_TYPES.RESET_STATE });
    hasInitialized.current = false;
    onClose();
  };

  const isLastStep = state.currentStep === 1;

  const actions = [
    {
      label: "Cancel",
      kind: "ghost" as const,
      onClick: handleClose,
      disabled: state.isDeploying,
    },
    {
      label: "Back",
      kind: "secondary" as const,
      onClick: handleBack,
      disabled: state.currentStep === 0 || state.isDeploying,
    },
    {
      label: isLastStep
        ? state.isDeploying
          ? "Deploying..."
          : "Deploy"
        : "Next",
      kind: "primary" as const,
      onClick: isLastStep ? handleSubmit : handleNext,
      disabled:
        state.isLoading || state.isDeploying || (isLastStep && state.isEditing),
    },
  ];

  return (
    <>
      {state.deployToastOpen && state.deployError && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title="Deployment failed"
          subtitle={state.deployError}
          onCloseButtonClick={() => {
            dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST });
          }}
          onActionButtonClick={async () => {
            dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST });
            await handleSubmit();
          }}
          className={styles.customToast}
        />
      )}
      <Tearsheet
        open={open}
        onClose={handleClose}
        title="Deploy digital assistant"
        actions={actions}
        className="customTearsheet"
        influencer={
          <div className={styles.influencerContent}>
            <ProgressIndicator currentIndex={state.currentStep} vertical>
              <ProgressStep
                label="Provide assistant details"
                description="Configure basic settings"
              />
              <ProgressStep
                label="Configure services"
                description="Select and configure services"
              />
            </ProgressIndicator>
          </div>
        }
        influencerPosition="left"
        influencerWidth="narrow"
      >
        <div className={styles.stepContent}>
          {state.isLoading ? (
            <div className={styles.loadingContainer}>
              <InlineLoading description="Loading deploy options..." />
            </div>
          ) : state.error ? (
            <div className={styles.errorContainer}>
              <p>Error: {state.error}</p>
            </div>
          ) : (
            <>
              {state.currentStep === 0 && deployOptions && (
                <StepOne
                  title="Provide assistant details"
                  formData={state.formData}
                  onChange={handleFormDataChange}
                  deployOptions={deployOptions}
                  showNameError={state.showStepOneNameError}
                />
              )}
              {state.currentStep === 1 && deployOptions && (
                <StepTwo
                  title="Configure services"
                  formData={state.formData}
                  onChange={handleFormDataChange}
                  deployOptions={deployOptions}
                  onEditingChange={handleEditingChange}
                  onResourceStatusChange={handleResourceStatusChange}
                />
              )}
            </>
          )}
        </div>
      </Tearsheet>
    </>
  );
};
