import { useReducer, useEffect, useRef, useMemo } from "react";
import { Tearsheet } from "@carbon/ibm-products";
import {
  ProgressIndicator,
  ProgressStep,
  InlineLoading,
  ActionableNotification,
} from "@carbon/react";
import type {
  ServicesDeployFlowProps,
  DeployFlowState,
  DeployFlowAction,
} from "./types.ts";
import type { ComponentConfig } from "../Shared/types";
import { ACTION_TYPES } from "./types.ts";
import {
  sharedDeployFlowReducer,
  useDeployFlowReducer,
} from "../Shared/hooks/useDeployFlowReducer";
import { deployApplication } from "@/api/applications.api";
import { transformToDeploymentPayload } from "./utils/serviceDeploymentTransform";
import { extractDeployError } from "../Shared/utils/deployError";
import { StepOne } from "./steps/StepOne";
import { StepTwo } from "./steps/StepTwo";
import { StepZero } from "./steps/StepZero";
import { useServiceDeployOptions } from "./hooks/useServiceDeployOptions";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { initializeFormData } from "./utils/formDataInitializer";
import { BASE_INITIAL_STATE } from "../Shared/utils/formData";
import styles from "./ServicesDeployFlow.module.scss";

// Initial state for the deployment flow
const getInitialState = (): DeployFlowState => ({
  ...BASE_INITIAL_STATE,
  formData: {
    name: "Service deployment (copy)",
    version: "",
    globalComponents: {},
    services: {},
  },
  selectedServiceId: null,
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
  const [state, dispatch] = useReducer(servicesDeployFlowReducer, {
    ...getInitialState(),
    selectedServiceId: preSelectedServiceId || null,
    currentStep: preSelectedServiceId ? 1 : 0, // Start at step 1 if service is pre-selected
  });

  // Track if form data has been initialized for the current service to prevent re-initialization
  const hasInitializedFormData = useRef<string | null>(null);
  // Track if we've already initialized on open to prevent re-initialization
  const hasInitializedOnOpen = useRef(false);

  // Only fetch deploy options when on step 1 or later (after user clicks Next)
  const shouldFetchDeployOptions =
    state.currentStep >= 1 && state.selectedServiceId;
  const { deployOptions, llmModels, isLoading, error } =
    useServiceDeployOptions(
      shouldFetchDeployOptions ? state.selectedServiceId : null,
    );

  // Get component models loading state from store
  const componentModelsLoading = useServiceDeployStore(
    (state) => state.componentModelsLoading,
  );

  // Get services from store to access service description
  const services = useServiceDeployStore((state) => state.services);

  // Find the selected service to get its description
  const selectedService = services?.find(
    (service) => service.id === state.selectedServiceId,
  );

  // Check if any Step 1 components are still loading their models
  const isStep1ComponentsLoading = useMemo(() => {
    if (!state.selectedServiceId || !deployOptions) return false;

    // Get Step 1 components (all except llm and reranker)
    const step1Components =
      deployOptions.components?.filter(
        (c) => c.type !== "llm" && c.type !== "reranker",
      ) || [];

    // Check if any of these components are still loading
    return step1Components.some((component) => {
      const key = `${state.selectedServiceId}:${component.type}`;
      return componentModelsLoading[key] === true;
    });
  }, [state.selectedServiceId, deployOptions, componentModelsLoading]);

  // Update state when tearsheet opens (only once per open)
  useEffect(() => {
    if (open && !hasInitializedOnOpen.current) {
      hasInitializedOnOpen.current = true;

      if (preSelectedServiceId) {
        dispatch({
          type: ACTION_TYPES.SET_SELECTED_SERVICE,
          payload: preSelectedServiceId,
        });
        dispatch({ type: ACTION_TYPES.SET_CURRENT_STEP, payload: 1 });
      } else {
        dispatch({ type: ACTION_TYPES.SET_CURRENT_STEP, payload: 0 });
      }
    } else if (!open) {
      // Reset when tearsheet closes
      hasInitializedOnOpen.current = false;
      hasInitializedFormData.current = null; // Reset form data initialization tracking
      dispatch({ type: ACTION_TYPES.RESET_STATE }); // Reset the entire state
    }
  }, [open, preSelectedServiceId]);

  // Initialize form data dynamically when deploy options are loaded (only once per service)
  useEffect(() => {
    if (
      open &&
      state.currentStep >= 1 &&
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

  // Get provider schemas and component models from store
  const providerSchemas = useServiceDeployStore(
    (state) => state.providerSchemas,
  );
  const componentModels = useServiceDeployStore(
    (state) => state.componentModels,
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
    const llmParams = llmComponent.params || {};

    // Check if all required fields have non-empty values
    return requiredFields.every((fieldKey) => {
      const value = llmParams[fieldKey];
      return (
        value !== undefined && value !== null && String(value).trim() !== ""
      );
    });
  }, [state.selectedServiceId, state.formData.services, providerSchemas]);

  // Set default model values from component models after they're fetched
  useEffect(() => {
    if (!open || !state.selectedServiceId || !deployOptions || isLoading) {
      return;
    }

    const serviceConfig = state.formData.services[state.selectedServiceId];
    if (!serviceConfig) return;

    // Check each component to see if it needs a default model value set
    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    deployOptions.components?.forEach((component) => {
      const componentConfig = serviceConfig.components[component.type];
      if (!componentConfig) return;

      // If component already has a model set, skip it
      if (componentConfig.params && componentConfig.params.model) return;

      // Get component models from store
      const componentKey = `${state.selectedServiceId}:${component.type}`;
      const models = componentModels[componentKey] || [];
      const selectedProviderId = componentConfig.providerId;
      const matchingModelForProvider = models.find(
        (model) => model.providerId === selectedProviderId,
      );

      // Only set a default model when it belongs to the currently selected provider
      if (matchingModelForProvider) {
        updates[component.type] = {
          ...componentConfig,
          params: {
            ...componentConfig.params,
            model: matchingModelForProvider.id,
          },
        };
        hasUpdates = true;
      }
    });

    // Apply updates if any
    if (hasUpdates) {
      dispatch({
        type: ACTION_TYPES.UPDATE_FORM_DATA,
        payload: {
          services: {
            ...state.formData.services,
            [state.selectedServiceId]: {
              ...serviceConfig,
              components: {
                ...serviceConfig.components,
                ...updates,
              },
            },
          },
        },
      });
    }
  }, [
    open,
    state.selectedServiceId,
    state.formData.services,
    deployOptions,
    isLoading,
    componentModels,
  ]);

  const {
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack,
  } = useDeployFlowReducer(dispatch, state.currentStep);

  const handleServiceSelect = (serviceId: string) => {
    // Service selection will trigger useServiceDeployOptions to fetch data
    dispatch({ type: ACTION_TYPES.SET_SELECTED_SERVICE, payload: serviceId });
  };

  const handleNext = () => {
    // Show validation error if trying to proceed from step 1 with invalid name
    if (state.currentStep === 1 && !state.formData.name.trim()) {
      dispatch({
        type: ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR,
        payload: true,
      });
      return;
    }

    if (state.currentStep < 2) {
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
      const deploymentPayload = await transformToDeploymentPayload(
        state.formData,
        deployOptions,
        providerSchemas,
        state.selectedServiceId,
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
    } finally {
      dispatch({ type: ACTION_TYPES.SET_IS_DEPLOYING, payload: false });
    }
  };

  const handleClose = () => {
    dispatch({ type: ACTION_TYPES.RESET_STATE });
    hasInitializedFormData.current = null; // Reset form data initialization tracking
    hasInitializedOnOpen.current = false; // Reset open initialization tracking
    onClose();
  };

  const isLastStep = state.currentStep === 2;

  // Determine if Next button should be disabled
  const isNextDisabled =
    state.isDeploying ||
    (state.currentStep === 0 && !state.selectedServiceId) || // Block on step 0 if no service selected
    (isLastStep && state.isEditing) ||
    (isLastStep && !areAllRequiredFieldsFilled); // Block deployment if required fields are not filled

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
      disabled: isNextDisabled,
    },
  ];

  return (
    <>
      {state.deployToastOpen && state.deployError && (
        <ActionableNotification
          kind="error"
          title="Deployment failed"
          subtitle={state.deployError}
          actionButtonLabel="Try again"
          closeOnEscape
          aria-label="close notification"
          onActionButtonClick={async () => {
            dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST });
            await handleSubmit();
          }}
          onCloseButtonClick={() => {
            dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST });
          }}
          className={styles.deployErrorNotification}
        />
      )}

      <Tearsheet
        open={open}
        onClose={handleClose}
        title="Deploy service"
        actions={actions}
        className="customTearsheet"
        influencer={
          <div className={styles.influencerContent}>
            <ProgressIndicator currentIndex={state.currentStep} vertical>
              <ProgressStep
                label="Select service"
                description="Choose a service to deploy"
                complete={!!state.selectedServiceId}
              />
              <ProgressStep
                label="Provide service details"
                description="Configure basic settings"
              />
              <ProgressStep
                label="Configure service"
                description="Select and configure service"
              />
            </ProgressIndicator>
          </div>
        }
        influencerPosition="left"
        influencerWidth="narrow"
      >
        <div className={styles.stepContent}>
          {state.currentStep === 0 && (
            <StepZero
              title="Select service"
              selectedServiceId={state.selectedServiceId}
              onServiceSelect={handleServiceSelect}
              isOpen={open}
            />
          )}

          {state.currentStep === 1 && (
            <>
              {error ? (
                <div className={styles.errorContainer}>
                  <p>Error: {error}</p>
                </div>
              ) : !deployOptions && isLoading ? (
                <div className={styles.loadingContainer}>
                  <InlineLoading description="Loading deploy options..." />
                </div>
              ) : isStep1ComponentsLoading ? (
                <div className={styles.loadingContainer}>
                  <InlineLoading description="Loading embedding models..." />
                </div>
              ) : deployOptions ? (
                <StepOne
                  title="Provide service details"
                  formData={state.formData}
                  onChange={handleFormDataChange}
                  deployOptions={deployOptions}
                  selectedServiceId={state.selectedServiceId}
                  showNameError={state.showStepOneNameError}
                />
              ) : null}
            </>
          )}

          {state.currentStep === 2 && (
            <>
              {error ? (
                <div className={styles.errorContainer}>
                  <p>Error: {error}</p>
                </div>
              ) : !deployOptions && isLoading ? (
                <div className={styles.loadingContainer}>
                  <InlineLoading description="Loading deploy options..." />
                </div>
              ) : deployOptions ? (
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
                />
              ) : null}
            </>
          )}
        </div>
      </Tearsheet>
    </>
  );
};
