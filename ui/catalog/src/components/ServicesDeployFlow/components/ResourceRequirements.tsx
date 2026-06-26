import { useState, useEffect, useMemo } from "react";
import {
  Tile,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
  InlineLoading,
  InlineNotification,
} from "@carbon/react";
import { Help, CheckmarkFilled, WarningFilled } from "@carbon/icons-react";
import {
  fetchResources,
  type ResourcesResponse,
  type ServiceDeployOptions,
} from "@/services/deployment.api";
import type { ResourceItem } from "../../DeployFlow/types/StepTwo.types";
import {
  bytesToGB,
  getResourceStatus,
} from "../../DeployFlow/utils/StepTwo.utils";
import type { DeployFormData } from "../types";
import styles from "../ServicesDeployFlow.module.scss";

interface ResourceRequirementsProps {
  formData: DeployFormData;
  deployOptions: ServiceDeployOptions;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
}

export const ResourceRequirements: React.FC<ResourceRequirementsProps> = ({
  formData,
  deployOptions,
  onResourceStatusChange,
}) => {
  const [resourceData, setResourceData] = useState<ResourcesResponse | null>(
    null,
  );
  const [resourcesLoading, setResourcesLoading] = useState(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  // Fetch available resources from API on mount
  useEffect(() => {
    const loadResources = async () => {
      try {
        setResourcesLoading(true);
        setResourcesError(null);

        const data = await fetchResources();
        setResourceData(data);
      } catch (error) {
        console.error("Failed to fetch resources:", error);

        if (error instanceof Error) {
          if (
            error.message.includes("401") ||
            error.message.includes("Authentication")
          ) {
            setResourcesError("Authentication failed. Please log in again.");
          } else if (
            error.message.includes("Network") ||
            error.message.includes("network")
          ) {
            setResourcesError("Network error. Please check your connection.");
          } else {
            setResourcesError(error.message);
          }
        } else {
          setResourcesError(
            "An unexpected error occurred while fetching resource data.",
          );
        }

        setResourceData(null);
      } finally {
        setResourcesLoading(false);
      }
    };

    loadResources();
  }, []);

  // Helper function to get accelerator label
  const getAcceleratorLabel = (acceleratorKey: string): string => {
    const labelMap: Record<string, string> = {
      "ibm.com/spyre_pf": "Accelerators",
    };

    return labelMap[acceleratorKey] || `Accelerators (${acceleratorKey})`;
  };

  // Calculate required resources based on selected services and providers
  const calculateRequiredResources = useMemo(() => {
    // Track unique providers to deduplicate resources
    // Shared providers run as single instances serving multiple services
    // Key: providerId-componentType (e.g., "vllm-cpu-embedding"), Value: provider resources
    const uniqueProviders: Record<
      string,
      {
        cpu: number;
        memory: number;
        storage: number;
        accelerators: Record<string, number>;
      }
    > = {};

    // Iterate through each enabled service
    Object.entries(formData.services).forEach(
      ([_serviceKey, serviceConfig]) => {
        if (!serviceConfig.enabled) return;

        // Add service-level resources (the service application itself)
        if (deployOptions.resources) {
          const serviceResourceKey = `service-${_serviceKey}`;
          if (!uniqueProviders[serviceResourceKey]) {
            uniqueProviders[serviceResourceKey] = {
              cpu: deployOptions.resources.cpu || 0,
              memory: deployOptions.resources.memory || 0,
              storage: deployOptions.resources.storage || 0,
              accelerators: { ...(deployOptions.resources.accelerators || {}) },
            };
          }
        }

        // Iterate through service-specific components dynamically
        Object.entries(serviceConfig.components).forEach(
          ([componentType, componentConfig]) => {
            const selectedProviderId = componentConfig.providerId;

            if (!selectedProviderId) return;

            // Find the component definition in deployOptions
            const component = deployOptions.components.find(
              (c) => c.type === componentType,
            );

            if (!component) return;

            const provider = component.providers.find(
              (p) => p.id === selectedProviderId,
            );

            // Create unique key combining provider ID and component type
            // This ensures vllm-cpu for embedding, reranker, and llm are counted separately
            const uniqueKey = `${selectedProviderId}-${componentType}`;

            if (provider?.resources && !uniqueProviders[uniqueKey]) {
              // First time seeing this provider-component combination - store its resources
              uniqueProviders[uniqueKey] = {
                cpu: provider.resources.cpu || 0,
                memory: provider.resources.memory || 0,
                storage: provider.resources.storage || 0,
                accelerators: { ...(provider.resources.accelerators || {}) },
              };
            }
          },
        );
      },
    );

    // Sum up resources from unique providers
    let totalCPU = 0;
    let totalMemory = 0;
    let totalStorage = 0;
    const totalAccelerators: Record<string, number> = {};

    Object.values(uniqueProviders).forEach((resources) => {
      totalCPU += resources.cpu;
      totalMemory += resources.memory;
      totalStorage += resources.storage;

      // Merge accelerators
      Object.entries(resources.accelerators).forEach(([key, count]) => {
        totalAccelerators[key] = (totalAccelerators[key] || 0) + count;
      });
    });

    // Convert memory from bytes to GB for display
    const memoryGB = Math.round(totalMemory / 1024 ** 3);
    const storageGB = Math.round(totalStorage / 1024 ** 3);

    return {
      cpu: totalCPU,
      memory: memoryGB,
      accelerators: totalAccelerators,
      storage: storageGB,
    };
  }, [formData.services, deployOptions.components, deployOptions.resources]);

  // Format resources for display
  const resourceRequirements = useMemo((): ResourceItem[] => {
    if (!resourceData) {
      return [];
    }

    const resources: ResourceItem[] = [];

    // 1. CPU (always present)
    resources.push({
      label: "Processors",
      required: calculateRequiredResources.cpu.toString(),
      available: Math.floor(resourceData.cpu.available_cpu).toString(),
      unit: "vCPUs",
      type: "cpu",
    });

    // 2. Memory (always present)
    resources.push({
      label: "Memory",
      required: calculateRequiredResources.memory.toString(),
      available: bytesToGB(resourceData.memory.available_bytes).toString(),
      unit: "GB",
      type: "memory",
    });

    // 3. Accelerators (may be empty object or contain multiple types)
    const acceleratorKeys = Object.keys(resourceData.accelerators);
    const totalRequired = Object.values(
      calculateRequiredResources.accelerators,
    ).reduce((sum, val) => sum + val, 0);

    if (acceleratorKeys.length > 0) {
      // Handle each accelerator type separately
      acceleratorKeys.forEach((acceleratorKey) => {
        const acceleratorData = resourceData.accelerators[acceleratorKey];
        const acceleratorLabel = getAcceleratorLabel(acceleratorKey);
        const requiredCount =
          calculateRequiredResources.accelerators[acceleratorKey] || 0;

        resources.push({
          label: acceleratorLabel,
          required: requiredCount.toString(),
          available: acceleratorData.available.toString(),
          unit: "Cards",
          type: "accelerator",
          acceleratorType: acceleratorKey,
        });
      });
    } else {
      // No accelerators available in system - always show with 0 available
      resources.push({
        label: "Accelerators",
        required: totalRequired.toString(),
        available: "0",
        unit: "Cards",
        type: "accelerator",
      });
    }

    // 4. Storage (not provided by API, show required only)
    if (calculateRequiredResources.storage > 0) {
      resources.push({
        label: "Disk storage",
        required: calculateRequiredResources.storage.toString(),
        available: "N/A",
        unit: "GB",
        type: "storage",
      });
    }

    return resources;
  }, [resourceData, calculateRequiredResources]);

  // Check for insufficient resources and notify parent
  useEffect(() => {
    if (!resourcesLoading && !resourcesError && resourceData) {
      const hasInsufficientResources = resourceRequirements.some((resource) => {
        const status = getResourceStatus(resource.required, resource.available);
        return status === "insufficient";
      });
      onResourceStatusChange?.(hasInsufficientResources);
    } else {
      // If resources are loading, in error state, or not available, consider it as insufficient
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
                <p className={styles.resourceLabel}>
                  <span>{resource.label}</span>
                  {status === "sufficient" && (
                    <CheckmarkFilled size={16} className={styles.green} />
                  )}
                  {status === "insufficient" && (
                    <WarningFilled size={16} className={styles.warning} />
                  )}
                </p>
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

// Made with Bob
