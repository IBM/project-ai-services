import { useMemo, useEffect } from "react";
import {
  Tile,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
  InlineLoading,
  InlineNotification,
  Tooltip,
} from "@carbon/react";
import { Help, CheckmarkFilled, WarningFilled } from "@carbon/icons-react";
import { bytesToGB } from "../../DigitalAssistant/utils/StepTwo.utils";
import { getResourceStatus } from "../utils/resourceStatus";
import type { ResourcesResponse } from "@/types/api.types";
import styles from "../deployFlow.shared.module.scss";
import type { ResourceItem } from "../types";

export interface CalculatedResources {
  cpu: number;
  memory: number;
  accelerators: Record<string, number>;
  storage: number;
}

export interface ResourceRequirementsPanelProps {
  calculatedResources: CalculatedResources;
  resourceData: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
}

export const ResourceRequirementsPanel: React.FC<
  ResourceRequirementsPanelProps
> = ({
  calculatedResources,
  resourceData,
  resourcesLoading,
  resourcesError,
  onResourceStatusChange,
}) => {
  const resourceRequirements = useMemo((): ResourceItem[] => {
    if (!resourceData) return [];

    const resources: ResourceItem[] = [];

    resources.push({
      label: "Processors",
      required: calculatedResources.cpu.toString(),
      available: Math.floor(resourceData.cpu.available_cpu).toString(),
      unit: "vCPUs",
      type: "cpu",
    });

    resources.push({
      label: "Memory",
      required: calculatedResources.memory.toString(),
      available: bytesToGB(resourceData.memory.available_bytes).toString(),
      unit: "GB",
      type: "memory",
    });

    const acceleratorKeys = Object.keys(resourceData.accelerators);
    const totalRequired = Object.values(
      calculatedResources.accelerators,
    ).reduce((sum, val) => sum + val, 0);

    if (acceleratorKeys.length > 0) {
      acceleratorKeys.forEach((acceleratorKey) => {
        const acceleratorData = resourceData.accelerators[acceleratorKey];
        resources.push({
          label: "Accelerators",
          required: (
            calculatedResources.accelerators[acceleratorKey] || 0
          ).toString(),
          available: acceleratorData.available.toString(),
          unit: "Cards",
          type: "accelerator",
          acceleratorType: acceleratorKey,
        });
      });
    } else {
      // No accelerators in system — always show with 0 available
      resources.push({
        label: "Accelerators",
        required: totalRequired.toString(),
        available: "0",
        unit: "Cards",
        type: "accelerator",
      });
    }

    if (calculatedResources.storage > 0) {
      resources.push({
        label: "Disk storage",
        required: calculatedResources.storage.toString(),
        available: "N/A",
        unit: "GB",
        type: "storage",
      });
    }

    return resources;
  }, [resourceData, calculatedResources]);

  useEffect(() => {
    if (!resourcesLoading && !resourcesError && resourceData) {
      const hasInsufficientResources = resourceRequirements.some(
        (r) => getResourceStatus(r.required, r.available) === "insufficient",
      );
      onResourceStatusChange?.(hasInsufficientResources);
    } else {
      onResourceStatusChange?.(true);
    }
  }, [
    resourceRequirements,
    resourcesLoading,
    resourcesError,
    resourceData,
    onResourceStatusChange,
  ]);

  return (
    <div className={styles.formSection}>
      <h3 className={styles.sectionTitle}>
        <div className={styles.labelWithInfo}>
          <span>Resource requirements</span>
          <Toggletip align="bottom">
            <ToggletipButton label="Additional information">
              <Help />
            </ToggletipButton>
            <ToggletipContent>
              <p>
                Digital assistant resource demands with the current service
                configuration and system status
              </p>
            </ToggletipContent>
          </Toggletip>
        </div>
      </h3>

      {/* Loading */}
      {resourcesLoading && (
        <div className={styles.resourceLoading}>
          <InlineLoading description="Loading resource information..." />
        </div>
      )}

      {/* Error */}
      {resourcesError && !resourcesLoading && (
        <InlineNotification
          kind="error"
          title="Resource data unavailable"
          subtitle={`Unable to retrieve system resource information: ${resourcesError}`}
          lowContrast
          hideCloseButton
        />
      )}

      {/* Success — resource tiles */}
      {!resourcesLoading && !resourcesError && resourceData && (
        <div className={styles.resourceGrid}>
          {resourceRequirements.map((resource) => {
            const status = getResourceStatus(
              resource.required,
              resource.available,
            );

            return (
              <Tile
                key={`${resource.label}-${resource.acceleratorType || ""}`}
                className={styles.resourceItem}
              >
                <div className={styles.resourceLabel}>
                  <span>{resource.label}</span>
                  {status === "sufficient" && (
                    <CheckmarkFilled size={16} className={styles.green} />
                  )}
                  {status === "insufficient" && (
                    <Tooltip
                      align="bottom"
                      label="Insufficient resources available"
                    >
                      <button
                        type="button"
                        className={styles.iconButton}
                        aria-label="Insufficient resources available"
                      >
                        <WarningFilled size={16} className={styles.warning} />
                      </button>
                    </Tooltip>
                  )}
                </div>
                <div className={styles.resourceValue}>
                  <span className={styles.required}>{resource.required}</span>
                  {resource.available !== "N/A" && (
                    <span className={styles.unit}>
                      /{resource.available} {resource.unit}
                    </span>
                  )}
                  {resource.available === "N/A" && (
                    <span className={styles.unit}> {resource.unit}</span>
                  )}
                </div>
              </Tile>
            );
          })}
        </div>
      )}

      {/* Empty — no data and no error */}
      {!resourcesLoading && !resourcesError && !resourceData && (
        <InlineNotification
          kind="info"
          title="Resource information not available"
          subtitle="System resource data could not be retrieved. Please try refreshing the page."
          lowContrast
          hideCloseButton
        />
      )}
    </div>
  );
};
