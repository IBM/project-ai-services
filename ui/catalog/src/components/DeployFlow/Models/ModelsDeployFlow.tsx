import { useReducer, useEffect, useCallback, useState } from "react";
import { useWorkers } from "@/hooks/useWorkers";
import { useModels } from "@/hooks/useModels";
import { DEFAULT_RUNTIME } from "@/constants";
import { createModel } from "@/api/models.api";
import type {
  ModelConfig,
  ModelsDeployFlowProps,
  ModelsDeployFlowState,
} from "./types";
import { ACTION_TYPES } from "./types";
import type { ModelsDeployFlowAction } from "./types";
import {
  sharedDeployFlowReducer,
  useDeployFlowReducer,
} from "../Shared/hooks/useDeployFlowReducer";
import { runDeployment } from "../Shared/utils/runDeployment";
import { DeployTearsheetShell } from "../Shared/components/DeployTearsheetShell";
import { SharedStepOne } from "../Shared/steps/SharedStepOne";
import {
  BASE_INITIAL_STATE,
  DEFAULT_FORM_DATA,
} from "../Shared/utils/formData";
import { RemoteModelConfig } from "./components/RemoteModelConfig";
import { loadModelDeployOptions } from "./hooks/useModelDeployOptions";
import { ModelsStepZero } from "./steps/ModelsStepZero";
import { ModelsStepTwo } from "./steps/ModelsStepTwo";
import {
  isValidModelName,
  MODEL_NAME_ERROR,
  MODEL_RUNTIME_OPTIONS,
} from "./utils/modelFlowOptions";
import { buildCreateModelPayload } from "./utils/modelDeploymentTransform";

const STEPS = [
  { label: "Select model" },
  { label: "Configure registration" },
  { label: "Configure model" },
];

const DEFAULT_MODEL_NAME = "Model-deployment";

const STEP_ONE = 1;
const STEP_TWO = 2;

const EMPTY_MODEL_CONFIG: ModelConfig = {
  inferenceBackendId: "",
  resourceProfileId: "",
  params: {},
};

// Builds the starting state
const getInitialState = (): ModelsDeployFlowState => ({
  ...BASE_INITIAL_STATE,
  formData: {
    name: DEFAULT_MODEL_NAME,
    version: "",
    globalComponents: {},
    services: {},
    ...DEFAULT_FORM_DATA,
  },
  selectedModelId: null,
  ...EMPTY_MODEL_CONFIG,
});

const modelsDeployFlowReducer = (
  state: ModelsDeployFlowState,
  action: ModelsDeployFlowAction,
): ModelsDeployFlowState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SELECTED_MODEL:
      // A new model invalidates the old backend and profile
      return state.selectedModelId === action.payload
        ? state
        : { ...state, selectedModelId: action.payload, ...EMPTY_MODEL_CONFIG };
    case ACTION_TYPES.SET_MODEL_CONFIG:
      return { ...state, ...action.payload };
    case ACTION_TYPES.RESET_STATE:
      return getInitialState();
    default:
      return sharedDeployFlowReducer(state, action);
  }
};

export const ModelsDeployFlow = ({
  open,
  onClose,
  onSubmit,
  preSelectedModelId,
}: ModelsDeployFlowProps) => {
  const {
    workers,
    isLoading: isLoadingWorkers,
    refetch: refetchWorkers,
  } = useWorkers();
  const {
    models,
    isLoading: isLoadingModels,
    error: modelsError,
  } = useModels(open);

  const [state, dispatch] = useReducer(
    modelsDeployFlowReducer,
    undefined,
    getInitialState,
  );

  // True when the applied backend, profile and params are valid
  const [isConfigValid, setIsConfigValid] = useState(false);
  // Remote field errors show once Register is clicked
  const [showRemoteErrors, setShowRemoteErrors] = useState(false);

  const isRemote = state.formData.deploymentType === "remote";
  // Remote has no Configure model step
  const lastStep = isRemote ? STEP_ONE : STEP_TWO;
  const selectedModel = models.find((m) => m.id === state.selectedModelId);

  // Prefetch the default runtime's options once a model is picked
  useEffect(() => {
    if (!open || !state.selectedModelId) return;
    // Errors surface in the config step's own fetch
    loadModelDeployOptions(state.selectedModelId, DEFAULT_RUNTIME).catch(
      () => undefined,
    );
  }, [open, state.selectedModelId]);

  useEffect(() => {
    if (open && preSelectedModelId) {
      dispatch({
        type: ACTION_TYPES.SET_SELECTED_MODEL,
        payload: preSelectedModelId,
      });
      dispatch({ type: ACTION_TYPES.SET_CURRENT_STEP, payload: STEP_ONE });
    } else if (!open) {
      dispatch({ type: ACTION_TYPES.RESET_STATE });
    }
  }, [open, preSelectedModelId]);

  const {
    handleNext,
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack: handleStepBack,
  } = useDeployFlowReducer(dispatch, state.currentStep, STEP_ONE, lastStep);

  // Goes back a step and clears remote errors so they don't reappear on return
  const handleBack = () => {
    setShowRemoteErrors(false);
    handleStepBack();
  };

  // Picks a model and clears remote errors left over from the previous one
  const handleModelSelect = (modelId: string) => {
    setShowRemoteErrors(false);
    dispatch({ type: ACTION_TYPES.SET_SELECTED_MODEL, payload: modelId });
  };

  const handleSubmit = async () => {
    if (isRemote) {
      const nameValid = isValidModelName(state.formData.name);
      if (!nameValid || !isConfigValid) {
        dispatch({
          type: ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR,
          payload: !nameValid,
        });
        setShowRemoteErrors(true);
        return;
      }
    }

    if (!selectedModel) {
      dispatch({
        type: ACTION_TYPES.SET_DEPLOY_ERROR,
        payload: "No model selected",
      });
      dispatch({ type: ACTION_TYPES.SHOW_DEPLOY_TOAST });
      return;
    }

    await runDeployment({
      dispatch,
      deploy: async () => {
        await createModel(
          buildCreateModelPayload({
            formData: state.formData,
            model: selectedModel,
            config: {
              inferenceBackendId: state.inferenceBackendId,
              resourceProfileId: state.resourceProfileId,
              params: state.params,
            },
          }),
        );
      },
      onSuccess: () => {
        onSubmit();
        dispatch({ type: ACTION_TYPES.RESET_STATE });
        setShowRemoteErrors(false);
        setIsConfigValid(false);
        onClose();
      },
      fallbackError: "Failed to register model",
    });
  };

  const handleClose = () => {
    dispatch({ type: ACTION_TYPES.RESET_STATE });
    setShowRemoteErrors(false);
    setIsConfigValid(false);
    onClose();
  };

  const handleWorkerErrorReset = useCallback(
    () =>
      dispatch({
        type: ACTION_TYPES.SET_SHOW_STEP_ONE_WORKER_ERROR,
        payload: false,
      }),
    [dispatch],
  );

  const handleApplyConfig = useCallback(
    (config: ModelConfig) =>
      dispatch({ type: ACTION_TYPES.SET_MODEL_CONFIG, payload: config }),
    [dispatch],
  );

  const isLastStep = state.currentStep === lastStep;

  // Remote stays enabled so Register can reveal errors
  const isPrimaryDisabled =
    (state.currentStep === 0 && !state.selectedModelId) ||
    (isLastStep && !isRemote && (state.isEditing || !isConfigValid));

  const stepsWithCompletion = [
    { ...STEPS[0], complete: !!state.selectedModelId },
    ...STEPS.slice(1, lastStep + 1),
  ];

  return (
    <DeployTearsheetShell
      open={open}
      onClose={handleClose}
      title="Register model"
      steps={stepsWithCompletion}
      currentStep={state.currentStep}
      isLastStep={isLastStep}
      isDeploying={state.isDeploying}
      isPrimaryDisabled={isPrimaryDisabled}
      onBack={handleBack}
      // An invalid name is passed as empty to trigger the shared name error
      onNext={() => {
        const name = isValidModelName(state.formData.name)
          ? state.formData.name
          : "";
        handleNext(name, state.formData.workerName);
      }}
      onSubmit={handleSubmit}
      deployError={state.deployError}
      deployToastOpen={state.deployToastOpen}
      onRetryDeploy={handleSubmit}
      onDismissToast={() => dispatch({ type: ACTION_TYPES.HIDE_DEPLOY_TOAST })}
      submitLabel="Register"
      submittingLabel="Registering..."
      errorTitle="Registration failed"
    >
      {state.currentStep === 0 && (
        <ModelsStepZero
          title="Select model"
          models={models}
          isLoading={isLoadingModels}
          error={modelsError}
          selectedModelId={state.selectedModelId}
          onModelSelect={handleModelSelect}
        />
      )}
      {state.currentStep === STEP_ONE && (
        <SharedStepOne
          title="Configure registration"
          formData={state.formData}
          onChange={handleFormDataChange}
          nameLabel="Model instance name"
          nameInvalidText={MODEL_NAME_ERROR}
          isNameValid={isValidModelName}
          deploymentTypeHeading="Model registration"
          runtimeOptions={MODEL_RUNTIME_OPTIONS}
          locationLabel="Location"
          locationTooltip="The host and partition or namespace where this deployment will run."
          showLocationHeading={false}
          remoteSection={
            isRemote ? (
              <RemoteModelConfig
                modelId={state.selectedModelId ?? ""}
                config={{
                  inferenceBackendId: state.inferenceBackendId,
                  resourceProfileId: state.resourceProfileId,
                  params: state.params,
                }}
                onApply={handleApplyConfig}
                onValidityChange={setIsConfigValid}
                showErrors={showRemoteErrors}
              />
            ) : undefined
          }
          showNameError={state.showStepOneNameError}
          showWorkerError={state.showStepOneWorkerError}
          onWorkerErrorReset={handleWorkerErrorReset}
          workers={workers}
          isLoadingWorkers={isLoadingWorkers}
          refetchWorkers={refetchWorkers}
        />
      )}
      {state.currentStep === STEP_TWO && !isRemote && (
        <ModelsStepTwo
          title="Configure model"
          modelId={state.selectedModelId ?? ""}
          modelName={selectedModel?.name ?? ""}
          modelDescription={selectedModel?.description ?? ""}
          componentType={selectedModel?.component_type.short_name ?? ""}
          deploymentType={state.formData.deploymentType}
          workerName={state.formData.workerName}
          config={{
            inferenceBackendId: state.inferenceBackendId,
            resourceProfileId: state.resourceProfileId,
            params: state.params,
          }}
          onApply={handleApplyConfig}
          onEditingChange={handleEditingChange}
          onResourceStatusChange={handleResourceStatusChange}
          onValidityChange={setIsConfigValid}
        />
      )}
    </DeployTearsheetShell>
  );
};
