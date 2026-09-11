import { useCallback, useEffect, useMemo, useReducer } from "react";
import {
  TextInput,
  Dropdown,
  Grid,
  Column,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
  InlineNotification,
  ClickableTile,
  IconButton,
} from "@carbon/react";
import { RadioButtonChecked, Information, Reset } from "@carbon/icons-react";
import { formatVersion } from "@/utils/string";
import styles from "../DeployFlow.shared.module.scss";
import type { DeployFormData, DeploymentRuntimeType } from "../types";
import type { WorkerApiResponse } from "@/types/api.types";
import { WORKER_RUNTIME_LABELS, LOCAL_WORKER_NAME } from "@/constants";
import RegisterWorkerModal from "@/components/WorkerResourcesTable/RegisterWorkerModal";
import type { RegisterPhase } from "@/components/WorkerResourcesTable/types";
import { registerWorker } from "@/api/workerResources.api";

interface RegisterState {
  isOpen: boolean;
  phase: RegisterPhase;
  workerName: string;
  token: string;
  gatewayAddress: string;
}

type RegisterAction =
  | { type: "OPEN" }
  | { type: "CLOSE" }
  | { type: "SET_NAME"; payload: string }
  | { type: "SET_PHASE"; payload: RegisterPhase }
  | { type: "SUCCESS"; payload: { token: string; gatewayAddress: string } };

const REGISTER_INITIAL: RegisterState = {
  isOpen: false,
  phase: "idle",
  workerName: "",
  token: "",
  gatewayAddress: "",
};

function registerReducer(
  state: RegisterState,
  action: RegisterAction,
): RegisterState {
  switch (action.type) {
    case "OPEN":
      return { ...REGISTER_INITIAL, isOpen: true };
    case "CLOSE":
      return { ...state, isOpen: false };
    case "SET_NAME":
      return {
        ...state,
        workerName: action.payload,
        phase: state.phase === "invalid" ? "idle" : state.phase,
      };
    case "SET_PHASE":
      return { ...state, phase: action.payload };
    case "SUCCESS":
      return {
        ...state,
        phase: "success",
        token: action.payload.token,
        gatewayAddress: action.payload.gatewayAddress,
      };
    default:
      return state;
  }
}

export interface StepOneComponentRow {
  type: string;
  name: string;
  hasModels: boolean; // true → model dropdown; false → provider dropdown
  modelOptions: Array<{ id: string; text: string }>;
  selectedModel: string;
  providerOptions: Array<{ id: string; text: string }>;
  selectedProviderId: string;
  description?: string;
}

export interface SharedStepOneProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  version: string;
  versionLabel: string;
  components: StepOneComponentRow[];
  onComponentChange: (componentType: string, providerId: string) => void;
  onModelChange?: (componentType: string, model: string) => void;
  showNameError?: boolean;
  showWorkerError?: boolean;
  onWorkerErrorReset?: () => void;
  failedComponentNames?: string[]; // Non-empty → renders an error banner listing the failed component names.
  onComponentError?: (hasError: boolean) => void;
  workers: WorkerApiResponse[];
  isLoadingWorkers: boolean;
  refetchWorkers: () => void;
}

export const SharedStepOne = ({
  title,
  formData,
  onChange,
  version,
  versionLabel,
  components,
  onComponentChange,
  onModelChange,
  showNameError = false,
  showWorkerError = false,
  onWorkerErrorReset,
  failedComponentNames = [],
  onComponentError,
  workers,
  isLoadingWorkers,
  refetchWorkers,
}: SharedStepOneProps) => {
  const isNameValid = !!formData.name.trim();
  const versionOptions = [{ id: version, text: formatVersion(version) }];

  const [registerState, dispatchRegister] = useReducer(
    registerReducer,
    REGISTER_INITIAL,
  );

  // Workers matching the selected runtime — Local is included when its runtime_type matches.
  // Only ready workers are shown; absent on --skip-local-worker installs.
  const filteredWorkers = useMemo(
    () =>
      workers.filter(
        (w) =>
          w.runtime_type === formData.deploymentType && w.status === "ready",
      ),
    [workers, formData.deploymentType],
  );

  // Local pinned first, then remaining remotes, then register sentinel.
  const workerOptions = useMemo(() => {
    const local = filteredWorkers.find((w) => w.name === LOCAL_WORKER_NAME);
    const remotes = filteredWorkers
      .filter((w) => w.name !== LOCAL_WORKER_NAME)
      .map((w) => ({ id: w.name, text: w.name }));
    const localOption = local ? [{ id: local.name, text: local.name }] : [];
    return [
      ...localOption,
      ...remotes,
      { id: "__register__", text: "Register worker resource" },
    ];
  }, [filteredWorkers]);

  // When the deployment type tile changes, reset workerName and clear any worker error.
  const handleDeploymentTypeChange = (value: string | number) => {
    const firstWorker = workers.find(
      (w) => w.runtime_type === value && w.status === "ready",
    );
    onChange({
      deploymentType: value as DeploymentRuntimeType,
      workerName: firstWorker?.name ?? "",
    });
    onWorkerErrorReset?.();
  };

  const handleWorkerDropdownChange = ({
    selectedItem,
  }: {
    selectedItem: { id: string; text: string } | null;
  }) => {
    if (!selectedItem) return;
    if (selectedItem.id === "__register__") {
      dispatchRegister({ type: "OPEN" });
      return;
    }
    onChange({ workerName: selectedItem.id });
  };

  const handleGenerateToken = useCallback(async () => {
    if (!registerState.workerName.trim()) {
      dispatchRegister({ type: "SET_PHASE", payload: "invalid" });
      return;
    }
    dispatchRegister({ type: "SET_PHASE", payload: "loading" });
    try {
      const result = await registerWorker(registerState.workerName.trim());
      dispatchRegister({
        type: "SUCCESS",
        payload: {
          token: result.token,
          gatewayAddress: result.gateway_address,
        },
      });
    } catch {
      dispatchRegister({ type: "SET_PHASE", payload: "error" });
    }
  }, [registerState.workerName]);

  // When modal closes after a successful registration, refresh the worker list.
  const handleModalClose = () => {
    const wasSuccess = registerState.phase === "success";
    dispatchRegister({ type: "CLOSE" });
    if (wasSuccess) refetchWorkers();
  };

  useEffect(() => {
    onComponentError?.(failedComponentNames.length > 0);
  }, [failedComponentNames, onComponentError]);

  // When the worker list refreshes, sync formData if the selected worker is no longer present.
  // Guarded by isLoadingWorkers so we don't reset mid-fetch while workers is still [].
  // Also clears the worker error when a valid worker is auto-selected.
  useEffect(() => {
    if (isLoadingWorkers) return;
    const realOptions = workerOptions.filter((w) => w.id !== "__register__");
    const exists = realOptions.some((w) => w.id === formData.workerName);
    if (!exists) {
      const next = realOptions[0]?.id ?? "";
      if (formData.workerName !== next) {
        onChange({ workerName: next });
        if (next) onWorkerErrorReset?.();
      }
    }
  }, [
    workerOptions,
    formData.workerName,
    onChange,
    isLoadingWorkers,
    onWorkerErrorReset,
  ]);

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      {failedComponentNames.length > 0 && (
        <InlineNotification
          kind="error"
          title={`Failed to load configurations of ${failedComponentNames.join(", ")}.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      <div className={`${styles.formSection} ${styles.formSectionStepOne}`}>
        <Grid narrow className={styles.formGrid}>
          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <TextInput
                id="deploy-name"
                labelText="Name"
                value={formData.name}
                invalid={showNameError && !isNameValid}
                invalidText="Name is required"
                onChange={(e) => onChange({ name: e.target.value })}
              />
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="deploy-version"
                titleText={versionLabel}
                label="Select version"
                items={versionOptions}
                itemToString={(item) => (item ? item.text : "")}
                selectedItem={
                  versionOptions.find((v) => v.id === formData.version) || null
                }
                onChange={({ selectedItem }) =>
                  onChange({ version: selectedItem?.id || "" })
                }
              />
            </div>
          </Column>

          {components.map((component) => {
            const labelNode = component.description ? (
              <div className={styles.labelWithInfo}>
                <span>{component.name}</span>
                <Toggletip align="top">
                  <ToggletipButton label="Additional information">
                    <Information />
                  </ToggletipButton>
                  <ToggletipContent>
                    <p>{component.description}</p>
                  </ToggletipContent>
                </Toggletip>
              </div>
            ) : (
              component.name
            );

            const dropdownProps = component.hasModels
              ? {
                  id: `${component.type}-model`,
                  items: component.modelOptions,
                  selectedItem:
                    component.modelOptions.find(
                      (m) => m.id === component.selectedModel,
                    ) || null,
                  onChange: ({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) => onModelChange?.(component.type, selectedItem?.id || ""),
                }
              : {
                  id: `${component.type}-provider`,
                  items: component.providerOptions,
                  selectedItem:
                    component.providerOptions.find(
                      (p) => p.id === component.selectedProviderId,
                    ) || null,
                  onChange: ({
                    selectedItem,
                  }: {
                    selectedItem: { id: string; text: string } | null;
                  }) =>
                    onComponentChange(component.type, selectedItem?.id || ""),
                };

            return (
              <Column key={component.type} sm={4} md={8} lg={16}>
                <div className={styles.formField}>
                  <Dropdown
                    {...dropdownProps}
                    titleText={labelNode}
                    label={`Select ${component.name.toLowerCase()}`}
                    itemToString={(item) => (item ? item.text : "")}
                  />
                </div>
              </Column>
            );
          })}

          <Column sm={4} md={8} lg={16}>
            <p className={styles.deploymentSectionHeading}>Deployment type</p>
            <div className={styles.deploymentTypeGrid}>
              {Object.entries(WORKER_RUNTIME_LABELS).map(
                ([value, { label, description, disabled }]) => {
                  const isSelected =
                    !disabled && formData.deploymentType === value;
                  return (
                    <ClickableTile
                      key={value}
                      id={`deployment-type-${value}`}
                      className={`${styles.deploymentTypeTile} ${
                        isSelected ? styles.deploymentTypeTileSelected : ""
                      } ${disabled ? styles.deploymentTypeTileDisabled : ""}`}
                      disabled={disabled}
                      onClick={() =>
                        !disabled && handleDeploymentTypeChange(value)
                      }
                    >
                      {isSelected && (
                        <div className={styles.deploymentTypeTileCheck}>
                          <RadioButtonChecked size={16} />
                        </div>
                      )}
                      <div className={styles.deploymentTypeTileContent}>
                        <p className={styles.deploymentTileLabel}>{label}</p>
                        <p className={styles.deploymentTileDescription}>
                          {description}
                        </p>
                      </div>
                    </ClickableTile>
                  );
                },
              )}
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.deploymentLocationField}>
              <p className={styles.deploymentSectionHeading}>
                Deployment location
              </p>
              <div className={styles.deploymentLocationLabel}>
                <span>Worker resource</span>
                <Toggletip align="top">
                  <ToggletipButton label="Worker resource information">
                    <Information />
                  </ToggletipButton>
                  <ToggletipContent>
                    <p>
                      A worker is a compute resource — such as a partition,
                      virtual machine, or OpenShift cluster — that runs AI
                      workloads on the platform. If the worker resource you need
                      isn&apos;t listed, click the Register worker resource
                      button to connect it to the platform.
                    </p>
                  </ToggletipContent>
                </Toggletip>
              </div>
              <div className={styles.deploymentLocationRow}>
                <div className={styles.workerDropdownWrapper}>
                  <Dropdown
                    id="deploy-location"
                    titleText=""
                    hideLabel
                    label="Choose an option"
                    items={workerOptions}
                    itemToString={(item) => (item ? item.text : "")}
                    disabled={isLoadingWorkers}
                    invalid={showWorkerError && !formData.workerName}
                    invalidText="Worker resource is required"
                    selectedItem={
                      workerOptions.find((w) => w.id === formData.workerName) ||
                      null
                    }
                    onChange={handleWorkerDropdownChange}
                  />
                </div>
                <IconButton
                  label="Refresh worker list"
                  kind="tertiary"
                  size="md"
                  className={styles.refreshButton}
                  onClick={refetchWorkers}
                >
                  <Reset />
                </IconButton>
              </div>
            </div>
          </Column>
        </Grid>
      </div>

      <RegisterWorkerModal
        isOpen={registerState.isOpen}
        phase={registerState.phase}
        workerName={registerState.workerName}
        token={registerState.token}
        gatewayAddress={registerState.gatewayAddress}
        onWorkerNameChange={(value) =>
          dispatchRegister({ type: "SET_NAME", payload: value })
        }
        onGenerateToken={() => void handleGenerateToken()}
        onClose={handleModalClose}
      />
    </>
  );
};
