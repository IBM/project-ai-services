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
import styles from "../DeployFlow.module.scss";
import { getResourceStatus } from "../utils/StepTwo.utils";
import type { ResourceItem } from "../types/StepTwo.types";

interface ResourceRequirementsProps {
  resourceRequirements: ResourceItem[];
  resourcesLoading: boolean;
  resourcesError: string | null;
  resourceData: boolean;
}

export const ResourceRequirements: React.FC<ResourceRequirementsProps> = ({
  resourceRequirements,
  resourcesLoading,
  resourcesError,
  resourceData,
}) => {
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

      {/* Loading State */}
      {resourcesLoading && (
        <div className={styles.resourceLoading}>
          <InlineLoading description="Loading resource information..." />
        </div>
      )}

      {/* Error State */}
      {resourcesError && !resourcesLoading && (
        <InlineNotification
          kind="error"
          title="Resource data unavailable"
          subtitle={`Unable to retrieve system resource information: ${resourcesError}`}
          lowContrast
          hideCloseButton
        />
      )}

      {/* Success State - Show Resources */}
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
                <p className={styles.resourceValue}>
                  <span className={styles.required}>{resource.required}</span>
                  {resource.available !== "N/A" && (
                    <span className={styles.unit}>
                      /{resource.available} {resource.unit}
                    </span>
                  )}
                  {resource.available === "N/A" && (
                    <span className={styles.unit}> {resource.unit}</span>
                  )}
                </p>
              </Tile>
            );
          })}
        </div>
      )}

      {/* Empty State - No data but no error */}
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
